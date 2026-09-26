// Package iconfetch proxies remote favicons with a short timeout, a
// size cap, and an in-memory TTL cache. It is the only external I/O
// in the app (port of the Rails IconFetch service).
package iconfetch

import (
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	timeout  = 2 * time.Second
	maxBytes = 64 * 1024
	cacheTTL = 24 * time.Hour
)

type entry struct {
	body []byte
	typ  string
	at   time.Time
}

// client never follows redirects (Net::HTTP doesn't either) and bounds
// the whole fetch with a short timeout.
var client = &http.Client{
	Timeout: timeout + time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
	Transport: &http.Transport{
		DialContext:           (&net.Dialer{Timeout: timeout}).DialContext,
		ResponseHeaderTimeout: timeout,
	},
}

var (
	mu    sync.Mutex
	cache = map[string]entry{}
)

// Call returns the cached (body, content-type) for url, downloading it
// on a miss. Failures return ok=false and are never cached.
func Call(rawurl string) (body []byte, typ string, ok bool) {
	key := strings.TrimSpace(rawurl)
	mu.Lock()
	e, hit := cache[key]
	mu.Unlock()
	if hit && time.Since(e.at) < cacheTTL {
		return e.body, e.typ, true
	}
	body, typ, ok = download(key)
	if !ok {
		return nil, "", false
	}
	mu.Lock()
	cache[key] = entry{body: body, typ: typ, at: time.Now()}
	// Opportunistic sweep so the map stays small.
	if len(cache)%256 == 0 {
		for k, v := range cache {
			if time.Since(v.at) >= cacheTTL {
				delete(cache, k)
			}
		}
	}
	mu.Unlock()
	return body, typ, true
}

func download(rawurl string) ([]byte, string, bool) {
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, "", false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, "", false
	}
	if u.User != nil || u.Hostname() == "" || privateHost(u.Hostname()) {
		return nil, "", false
	}
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", false
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, "", false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil || len(body) > maxBytes {
		return nil, "", false
	}
	typ := strings.Split(resp.Header.Get("Content-Type"), ";")[0]
	typ = strings.TrimSpace(typ)
	if typ == "" {
		typ = "image/png"
	}
	return body, typ, true
}

// privateHost mirrors IconFetch.private_host?: literal loopback, private,
// and link-local IPs plus localhost names are rejected. Plain hostnames
// pass (no DNS resolution, like the Rails version).
func privateHost(host string) bool {
	name := strings.ToLower(strings.TrimSuffix(host, "."))
	if name == "localhost" || strings.HasSuffix(name, ".localhost") || name == "unix" {
		return true
	}
	ip := net.ParseIP(name)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}
