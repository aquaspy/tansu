// Package openrouter talks to the OpenRouter Chat Completions API.
package openrouter

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const baseURL = "https://openrouter.ai/api/v1"

// Error is a failed API call; TimeoutError is a network timeout.
// Code carries the HTTP or provider error code when known (0 otherwise).
type Error struct {
	Msg  string
	Code int
}

func (e *Error) Error() string { return e.Msg }

type TimeoutError struct{ Msg string }

func (e *TimeoutError) Error() string { return e.Msg }

// Retryable reports whether err looks transient (rate limits, overloaded
// or flapping providers, network timeouts) and worth another attempt.
// Anything else (bad key, bad request, unknown model) fails fast.
func Retryable(err error) bool {
	var terr *TimeoutError
	if errors.As(err, &terr) {
		return true
	}
	var oerr *Error
	if !errors.As(err, &oerr) {
		return false
	}
	switch oerr.Code {
	case 429, 500, 502, 503, 504:
		return true
	}
	msg := strings.ToLower(oerr.Msg)
	for _, hint := range []string{"rate limit", "rate-limit", "rate_limit", "ratelimit", "too many requests", "overloaded", "temporarily unavailable"} {
		if strings.Contains(msg, hint) {
			return true
		}
	}
	return false
}

type Client struct {
	apiKey string
	model  string
	http   *http.Client
	base   string
}

func New(apiKey, model string) (*Client, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, &Error{Msg: "missing_key"}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: 10 * time.Second}).DialContext
	return &Client{
		apiKey: apiKey,
		model:  model,
		http:   &http.Client{Transport: transport, Timeout: time.Hour},
		base:   baseURL,
	}, nil
}

func (c *Client) body(messages []any, maxCompletionTokens *int, reasoningEffort string, stream bool, sessionID string, search *SearchOptions, files *FileOptions) map[string]any {
	b := map[string]any{
		"model":    c.model,
		"messages": messages,
		"stream":   stream,
		"provider": map[string]any{"zdr": true},
	}
	if reasoningEffort != "" {
		b["reasoning"] = map[string]any{"effort": reasoningEffort}
	}
	if maxCompletionTokens != nil {
		b["max_completion_tokens"] = *maxCompletionTokens
	}
	if sessionID != "" {
		b["session_id"] = sessionID
	}
	var plugins []any
	if search != nil {
		plugins = append(plugins, search.plugin())
	}
	if files != nil {
		plugins = append(plugins, files.plugin())
	}
	if len(plugins) > 0 {
		b["plugins"] = plugins
	}
	return b
}

// StreamChat posts a streaming request and yields each SSE payload.
// sessionID pins OpenRouter's sticky routing so one conversation keeps a
// warm provider cache. A non-nil search attaches the web plugin (exactly
// one search per request); a non-nil files attaches the file-parser
// plugin. Cancel ctx to drop the TCP/TLS connection immediately
// (generation and billing stop). A non-nil yield error stops the stream
// and is returned.
func (c *Client) StreamChat(ctx context.Context, messages []any, maxCompletionTokens *int, reasoningEffort, sessionID string, search *SearchOptions, files *FileOptions, yield func(map[string]any) error) error {
	return c.post(ctx, "/chat/completions",
		c.body(messages, maxCompletionTokens, reasoningEffort, true, sessionID, search, files),
		yield)
}

// Complete posts a non-streaming request (titles, compaction; never search).
func (c *Client) Complete(ctx context.Context, messages []any, maxCompletionTokens *int, reasoningEffort string) (map[string]any, error) {
	var out map[string]any
	err := c.post(ctx, "/chat/completions",
		c.body(messages, maxCompletionTokens, reasoningEffort, false, "", nil, nil),
		func(ev map[string]any) error { out = ev; return nil })
	if err != nil {
		return nil, err
	}
	if e, ok := out["error"].(map[string]any); ok && e != nil {
		msg, _ := e["message"].(string)
		if msg == "" {
			msg = "generation_failed"
		}
		return nil, &Error{Msg: msg}
	}
	return out, nil
}

// MessageText extracts the assistant text from a completed response.
func MessageText(response map[string]any) string {
	choices, _ := response["choices"].([]any)
	if len(choices) == 0 {
		return ""
	}
	first, _ := choices[0].(map[string]any)
	msg, _ := first["message"].(map[string]any)
	if msg == nil {
		return ""
	}
	text, _ := msg["content"].(string)
	return text
}

func (c *Client) post(ctx context.Context, path string, body map[string]any, yield func(map[string]any) error) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, strings.NewReader(string(raw)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Title", "TansuChat")
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return &TimeoutError{Msg: "timeout"}
		}
		var nerr net.Error
		if errors.As(err, &nerr) && nerr.Timeout() {
			return &TimeoutError{Msg: "timeout"}
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &Error{Msg: fmt.Sprintf("http_%d", resp.StatusCode), Code: resp.StatusCode}
	}
	if yield == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if streamed, _ := body["stream"].(bool); streamed {
		return readSSE(resp.Body, yield)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	yield(out)
	return nil
}

// readSSE parses the event stream: strip \r, split on blank lines, join
// data: lines, skip comment keep-alives and [DONE].
func readSSE(r io.Reader, yield func(map[string]any) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	var data strings.Builder
	flush := func() error {
		payload := strings.TrimSpace(data.String())
		data.Reset()
		if payload == "" || payload == "[DONE]" {
			return nil
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			return &Error{Msg: "bad_frame"}
		}
		return yield(ev)
	}
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if v, ok := strings.CutPrefix(line, "data:"); ok {
			data.WriteString(strings.TrimSpace(v))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}
