package ics

import (
	"context"
	"net"
	"strings"
	"testing"
)

func TestAddressAllowed(t *testing.T) {
	ok := []string{"8.8.8.8", "1.1.1.1", "2001:4860:4860::8888"}
	bad := []string{
		"127.0.0.1", "::1", "10.1.2.3", "192.168.1.1", "172.16.0.1",
		"169.254.169.254", "0.0.0.0", "100.64.1.1", "192.0.2.1",
		"198.51.100.1", "203.0.113.5", "224.0.0.1", "255.255.255.255",
		"fc00::1", "fe80::1", "2001:db8::1",
	}
	for _, s := range ok {
		if !AddressAllowed(net.ParseIP(s)) {
			t.Errorf("%s should be allowed", s)
		}
	}
	for _, s := range bad {
		if AddressAllowed(net.ParseIP(s)) {
			t.Errorf("%s should be blocked", s)
		}
	}
}

type fakeRes struct {
	ips []net.IP
	err error
}

func (f fakeRes) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]net.IPAddr, len(f.ips))
	for i, ip := range f.ips {
		out[i] = net.IPAddr{IP: ip}
	}
	return out, nil
}

func TestCheckURL(t *testing.T) {
	ctx := context.Background()
	secret := "https://user:secret-token@127.0.0.1/path/secret-token.ics"
	if err := CheckURL(ctx, secret, fakeRes{}); err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("err = %v", err)
	}
	cases := []struct {
		raw string
		res fakeRes
		err error
	}{
		{"http://example.com/a.ics", fakeRes{ips: []net.IP{net.ParseIP("8.8.8.8")}}, ErrURL},
		{"https://127.0.0.1/a.ics", fakeRes{}, ErrBlocked},
		{"https://10.0.0.5/a.ics", fakeRes{}, ErrBlocked},
		{"https://169.254.169.254/latest", fakeRes{}, ErrBlocked},
		{"https://[::1]/a.ics", fakeRes{}, ErrBlocked},
		{"https://localhost/a.ics", fakeRes{}, ErrBlocked},
		{"https://metadata.google.internal/a", fakeRes{}, ErrBlocked},
		{"https://feeds.example/secret-token/a.ics", fakeRes{ips: []net.IP{net.ParseIP("10.1.1.1")}}, ErrBlocked},
		{"https://feeds.example/secret-token/a.ics", fakeRes{ips: []net.IP{net.ParseIP("8.8.8.8"), net.ParseIP("127.0.0.1")}}, ErrBlocked},
		{"https://feeds.example/a.ics", fakeRes{ips: []net.IP{net.ParseIP("8.8.8.8")}}, nil},
	}
	for _, tc := range cases {
		err := CheckURL(ctx, tc.raw, tc.res)
		if tc.err == nil && err != nil {
			t.Errorf("%s: %v", tc.raw, err)
		}
		if tc.err != nil && err != tc.err {
			t.Errorf("%s: got %v want %v", tc.raw, err, tc.err)
		}
		if err != nil && strings.Contains(err.Error(), "secret-token") {
			t.Errorf("error leaked url: %v", err)
		}
	}
}
