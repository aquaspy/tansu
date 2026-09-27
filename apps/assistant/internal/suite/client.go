package suite

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const callTimeout = 10 * time.Second

// ErrRedirect is a 3xx from an app that answers in place.
var ErrRedirect = errors.New("redirect")

// App is one configured sibling.
type App struct {
	Name string // notes, calendar, spend, people
	Base string // origin, no trailing slash
}

// Client calls one sibling's /api/v1. It never follows a redirect.
type Client struct {
	App   App
	Token string
	HTTP  *http.Client
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{
		Timeout: callTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// Call performs one API request. path is a rooted /api/v1 path with an
// optional query. A 3xx comes back as ErrRedirect plus the status.
func (c *Client) Call(ctx context.Context, method, path string, body any) (int, []byte, error) {
	if !strings.HasPrefix(path, "/api/v1/") || strings.Contains(path, "://") || strings.Contains(path, "..") {
		return 0, nil, fmt.Errorf("path")
	}
	base, err := url.Parse(c.App.Base)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return 0, nil, fmt.Errorf("base")
	}
	u, err := url.Parse(c.App.Base + path)
	if err != nil || !strings.EqualFold(u.Scheme, base.Scheme) || !strings.EqualFold(u.Host, base.Host) {
		return 0, nil, fmt.Errorf("base")
	}
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rdr)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return resp.StatusCode, raw, ErrRedirect
	}
	return resp.StatusCode, raw, nil
}

// Lost reports a timeout or a dropped connection after the request was sent.
func Lost(err error) bool {
	if err == nil || errors.Is(err, ErrRedirect) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var nerr interface{ Timeout() bool }
	if errors.As(err, &nerr) && nerr.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "connection") || strings.Contains(msg, "eof")
}
