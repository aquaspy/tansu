package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Transcript is one /audio/transcriptions result. Seconds and CostUSD come
// from the response usage block (0 when the provider omits them).
type Transcript struct {
	Text    string
	Seconds float64
	CostUSD float64
}

// Transcribe posts audio to /audio/transcriptions (OpenAI-compatible
// multipart) and returns the recognized text. Language is a BCP-47 tag
// ("" lets the provider detect it). Upstream providers time out past
// ~60s of processing, so callers cap recordings well below that.
func (c *Client) Transcribe(ctx context.Context, audio []byte, filename, language string) (Transcript, error) {
	var out Transcript
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("model", c.model); err != nil {
		return out, err
	}
	if language != "" {
		if err := w.WriteField("language", language); err != nil {
			return out, err
		}
	}
	if err := w.WriteField("provider", `{"zdr":true}`); err != nil {
		return out, err
	}
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return out, err
	}
	if _, err := part.Write(audio); err != nil {
		return out, err
	}
	if err := w.Close(); err != nil {
		return out, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/audio/transcriptions", &body)
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Title", "TansuChat")
	resp, err := c.http.Do(req)
	if err != nil {
		return out, audioErr(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return out, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return out, &Error{Msg: fmt.Sprintf("http_%d", resp.StatusCode), Code: resp.StatusCode}
	}
	var decoded struct {
		Text  string `json:"text"`
		Usage struct {
			Seconds float64 `json:"seconds"`
			Cost    float64 `json:"cost"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return out, err
	}
	out.Text = strings.TrimSpace(decoded.Text)
	out.Seconds = decoded.Usage.Seconds
	out.CostUSD = decoded.Usage.Cost
	return out, nil
}

// Speak posts text to /audio/speech and returns the raw audio stream.
// The caller closes the body. genID feeds GenerationCost for metering
// ("" when the provider omits the header).
func (c *Client) Speak(ctx context.Context, text, voice, format string) (body io.ReadCloser, genID string, err error) {
	raw, err := json.Marshal(map[string]any{
		"model": c.model, "input": text, "voice": voice, "response_format": format,
		"provider": map[string]any{"zdr": true},
	})
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/audio/speech", bytes.NewReader(raw))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Title", "TansuChat")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", audioErr(err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		resp.Body.Close()
		if msg := audioErrorMessage(b); msg != "" {
			return nil, "", &Error{Msg: msg, Code: resp.StatusCode}
		}
		return nil, "", &Error{Msg: fmt.Sprintf("http_%d", resp.StatusCode), Code: resp.StatusCode}
	}
	return resp.Body, resp.Header.Get("X-Generation-Id"), nil
}

// GenerationCost reads the billed total for one generation id. Audio costs
// post asynchronously: a 404 here means "not ready yet", not failure.
func (c *Client) GenerationCost(ctx context.Context, genID string) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.base+"/generation?id="+url.QueryEscape(genID), nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, audioErr(err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, &Error{Msg: fmt.Sprintf("http_%d", resp.StatusCode), Code: resp.StatusCode}
	}
	var decoded struct {
		Data struct {
			TotalCost float64 `json:"total_cost"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return 0, err
	}
	return decoded.Data.TotalCost, nil
}

// audioErrorMessage pulls the provider message out of an error JSON body.
func audioErrorMessage(body []byte) string {
	var decoded struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return ""
	}
	return strings.TrimSpace(decoded.Error.Message)
}

func audioErr(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &TimeoutError{Msg: "timeout"}
	}
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		return &TimeoutError{Msg: "timeout"}
	}
	return err
}

// SpeakTimeout bounds one TTS request; generation polling uses PollDelays.
const SpeakTimeout = 90 * time.Second

// PollDelays spaces generation-cost retries (costs post asynchronously).
var PollDelays = []time.Duration{3 * time.Second, 5 * time.Second, 8 * time.Second}
