package iconfetch

import (
	"testing"
)

func TestRejectsLocalAndNonHTTPURLs(t *testing.T) {
	for _, raw := range []string{
		"javascript:alert(1)",
		"http://127.0.0.1/secret",
		"http://localhost/secret",
		"http://192.168.0.1/x",
		"http://10.0.0.5/x",
		"http://[::1]/x",
		"http://user@example.com/x",
		"ftp://example.com/x",
		"",
		"not a url",
	} {
		if _, _, ok := Call(raw); ok {
			t.Fatalf("Call(%q) accepted", raw)
		}
	}
}

func TestPrivateHost(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost":      true,
		"LOCALHOST":      true,
		"x.localhost":    true,
		"unix":           true,
		"127.0.0.1":      true,
		"10.1.2.3":       true,
		"192.168.1.1":    true,
		"172.16.0.1":     true,
		"169.254.10.20":  true,
		"::1":            true,
		"example.com":    false,
		"8.8.8.8":        false,
		"1.1.1.1":        false,
		"not-an-ip":      false,
		"localhostx.com": false,
	} {
		if got := privateHost(host); got != want {
			t.Fatalf("privateHost(%q) = %v, want %v", host, got, want)
		}
	}
}
