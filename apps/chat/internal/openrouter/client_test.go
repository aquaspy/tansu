package openrouter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

type capture struct {
	path    string
	payload map[string]any
	headers http.Header
	sse     string
	json    string
	status  int
}

func captureServer(t *testing.T, c *capture) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.path = r.URL.Path
		c.headers = r.Header
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &c.payload)
		if c.status != 0 {
			w.WriteHeader(c.status)
			return
		}
		if c.sse != "" {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(c.sse))
			return
		}
		_, _ = w.Write([]byte(c.json))
	}))
}

func testClient(t *testing.T, srv *httptest.Server, model string) *Client {
	t.Helper()
	c, err := New("x", model)
	if err != nil {
		t.Fatal(err)
	}
	c.base = srv.URL
	return c
}

func TestStreamBody(t *testing.T) {
	var c capture
	srv := captureServer(t, &c)
	defer srv.Close()
	client := testClient(t, srv, "openai/gpt-6-luna")
	err := client.StreamChat(context.Background(),
		[]any{map[string]any{"role": "user", "content": "Hi"}}, nil, "high", "kura-9", nil, nil,
		func(map[string]any) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if c.path != "/chat/completions" {
		t.Fatalf("path = %q", c.path)
	}
	if c.payload["model"] != "openai/gpt-6-luna" || c.payload["stream"] != true {
		t.Fatalf("payload = %v", c.payload)
	}
	msgs, _ := c.payload["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages = %v", c.payload["messages"])
	}
	if !reflect.DeepEqual(c.payload["reasoning"], map[string]any{"effort": "high"}) {
		t.Fatalf("reasoning = %v", c.payload["reasoning"])
	}
	if !reflect.DeepEqual(c.payload["provider"], map[string]any{"zdr": true}) {
		t.Fatalf("provider = %v", c.payload["provider"])
	}
	if c.payload["session_id"] != "kura-9" {
		t.Fatalf("session = %v", c.payload["session_id"])
	}
	if _, ok := c.payload["max_completion_tokens"]; ok {
		t.Fatalf("payload has max_completion_tokens")
	}
	if c.headers.Get("Authorization") != "Bearer x" {
		t.Fatalf("auth = %q", c.headers.Get("Authorization"))
	}
	if c.headers.Get("X-Title") != "TansuChat" {
		t.Fatalf("title = %q", c.headers.Get("X-Title"))
	}
}

func TestStreamMaxTokens(t *testing.T) {
	var c capture
	srv := captureServer(t, &c)
	defer srv.Close()
	client := testClient(t, srv, "openai/gpt-6-luna")
	maxOut := 16000
	err := client.StreamChat(context.Background(), []any{}, &maxOut, "high", "",
		nil, nil,
		func(map[string]any) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if c.payload["max_completion_tokens"] != 16000.0 {
		t.Fatalf("max = %v", c.payload["max_completion_tokens"])
	}
	if _, ok := c.payload["session_id"]; ok {
		t.Fatalf("payload has session_id")
	}
}

func TestCompleteBody(t *testing.T) {
	var c capture
	c.json = `{"id":"gen-1","choices":[{"message":{"role":"assistant","content":"T"}}],"usage":{"prompt_tokens":5}}`
	srv := captureServer(t, &c)
	defer srv.Close()
	client := testClient(t, srv, "openai/gpt-6-luna")
	maxOut := 24
	out, err := client.Complete(context.Background(),
		[]any{map[string]any{"role": "user", "content": "Title me"}}, &maxOut, "none")
	if err != nil {
		t.Fatal(err)
	}
	if MessageText(out) != "T" {
		t.Fatalf("text = %q", MessageText(out))
	}
	if c.payload["stream"] != false {
		t.Fatalf("payload = %v", c.payload)
	}
	if c.payload["max_completion_tokens"] != 24.0 {
		t.Fatalf("max = %v", c.payload["max_completion_tokens"])
	}
	if _, ok := c.payload["plugins"]; ok {
		t.Fatalf("Complete must never search: %v", c.payload["plugins"])
	}
	if !reflect.DeepEqual(c.payload["reasoning"], map[string]any{"effort": "none"}) {
		t.Fatalf("reasoning = %v", c.payload["reasoning"])
	}
	if _, ok := c.payload["session_id"]; ok {
		t.Fatalf("payload has session_id")
	}
}

func TestCompleteErrorObject(t *testing.T) {
	var c capture
	c.json = `{"id":"gen-1","error":{"code":429,"message":"temporarily rate-limited upstream"}}`
	srv := captureServer(t, &c)
	defer srv.Close()
	client := testClient(t, srv, "openai/gpt-6-luna")
	_, err := client.Complete(context.Background(), []any{}, nil, "none")
	if err == nil || err.Error() != "temporarily rate-limited upstream" {
		t.Fatalf("err = %v", err)
	}
}

func TestMissingKey(t *testing.T) {
	if _, err := New("", "openai/gpt-6-luna"); err == nil || err.Error() != "missing_key" {
		t.Fatalf("err = %v", err)
	}
}

func TestMessageText(t *testing.T) {
	if got := MessageText(map[string]any{}); got != "" {
		t.Fatalf("empty = %q", got)
	}
	if got := MessageText(map[string]any{"choices": []any{}}); got != "" {
		t.Fatalf("no choices = %q", got)
	}
}

func TestSSEParsing(t *testing.T) {
	var c capture
	c.sse = ": OPENROUTER PROCESSING\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"\",\"reasoning\":\"thinking\"},\"finish_reason\":null}]}\r\n\r\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"model\":\"m\",\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":1,\"cost\":0.000001}}\n\n" +
		"data: [DONE]\n\n"
	srv := captureServer(t, &c)
	defer srv.Close()
	client := testClient(t, srv, "openai/gpt-6-luna")
	var got []map[string]any
	err := client.StreamChat(context.Background(), []any{}, nil, "high", "",
		nil, nil,
		func(ev map[string]any) error {
			got = append(got, ev)
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("events = %d", len(got))
	}
	usage, _ := got[2]["usage"].(map[string]any)
	if usage["cost"] != 0.000001 {
		t.Fatalf("usage = %v", got[2]["usage"])
	}
}

func TestStreamErrorChunkYielded(t *testing.T) {
	var c capture
	c.sse = "data: {\"choices\":[],\"error\":{\"code\":429,\"message\":\"rate-limited\"}}\n\n"
	srv := captureServer(t, &c)
	defer srv.Close()
	client := testClient(t, srv, "openai/gpt-6-luna")
	var got []map[string]any
	err := client.StreamChat(context.Background(), []any{}, nil, "high", "",
		nil, nil,
		func(ev map[string]any) error {
			got = append(got, ev)
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0]["error"] == nil {
		t.Fatalf("got = %v", got)
	}
}

func TestHTTPError(t *testing.T) {
	var c capture
	c.status = 429
	srv := captureServer(t, &c)
	defer srv.Close()
	client := testClient(t, srv, "openai/gpt-6-luna")
	err := client.StreamChat(context.Background(), []any{}, nil, "high", "",
		nil, nil,
		func(map[string]any) error { return nil })
	if err == nil || err.Error() != "http_429" {
		t.Fatalf("err = %v", err)
	}
}

func TestRetryable(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{&Error{Msg: "http_429", Code: 429}, true},
		{&Error{Msg: "http_503", Code: 503}, true},
		{&Error{Msg: "temporarily rate-limited upstream"}, true},
		{&Error{Msg: "Rate limit exceeded"}, true},
		{&Error{Msg: "provider overloaded"}, true},
		{&TimeoutError{Msg: "timeout"}, true},
		{&Error{Msg: "http_400", Code: 400}, false},
		{&Error{Msg: "http_401", Code: 401}, false},
		{&Error{Msg: "missing_key"}, false},
		{&Error{Msg: "generation_failed"}, false},
		{&Error{Msg: "bad_frame"}, false},
	}
	for _, c := range cases {
		if got := Retryable(c.err); got != c.want {
			t.Errorf("Retryable(%q) = %v, want %v", c.err, got, c.want)
		}
	}
}
