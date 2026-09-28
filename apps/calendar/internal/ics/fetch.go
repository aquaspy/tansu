package ics

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxBody = 4 << 20

var (
	// ErrURL is a link we will not store: not https, or missing a host.
	ErrURL = errors.New("ics url")
	// ErrBlocked is a private, link-local, loopback, or metadata address.
	ErrBlocked = errors.New("ics blocked")
	// ErrFetch is a network or HTTP failure. The error text never includes
	// the URL, because feed links often carry a secret in the path.
	ErrFetch = errors.New("ics fetch")
	// ErrLarge is a body over maxBody.
	ErrLarge = errors.New("ics large")
	// ErrParse is a body that is not a VCALENDAR.
	ErrParse = errors.New("ics parse")
)

// Resolver looks up a hostname. Tests pass a fake; production passes nil
// for the system resolver.
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// CheckURLShape is the part that does not need DNS: https, no userinfo,
// no IP literal we already know is unsafe. The test fetch hook still uses
// this so a stub cannot be pointed at loopback.
func CheckURLShape(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || u.Scheme != "https" || u.User != nil {
		return ErrURL
	}
	if len(raw) > 2048 {
		return ErrURL
	}
	host := u.Hostname()
	if _, err := strconv.Atoi(host); err == nil {
		return ErrBlocked
	}
	low := strings.ToLower(strings.TrimSuffix(host, "."))
	if low == "localhost" || strings.HasSuffix(low, ".localhost") ||
		low == "metadata.google.internal" || strings.HasSuffix(low, ".internal") ||
		low == "metadata" {
		return ErrBlocked
	}
	if ip := net.ParseIP(host); ip != nil && !AddressAllowed(ip) {
		return ErrBlocked
	}
	return nil
}

// CheckURL is CheckURLShape plus a DNS lookup. Every returned address must
// be a public unicast address.
func CheckURL(ctx context.Context, raw string, res Resolver) error {
	if err := CheckURLShape(raw); err != nil {
		return err
	}
	u, _ := url.Parse(strings.TrimSpace(raw))
	host := u.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		return nil
	}
	if res == nil {
		res = net.DefaultResolver
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addrs, err := res.LookupIPAddr(ctx, host)
	if err != nil || len(addrs) == 0 {
		return ErrFetch
	}
	for _, a := range addrs {
		if !AddressAllowed(a.IP) {
			return ErrBlocked
		}
	}
	return nil
}

// AddressAllowed reports whether ip is a public unicast address we will dial.
func AddressAllowed(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsPrivate() ||
		!ip.IsGlobalUnicast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 0:
			return false
		case v4[0] == 100 && v4[1]&0xc0 == 64:
			return false
		case v4[0] == 192 && v4[1] == 0 && v4[2] == 0:
			return false
		case v4[0] == 192 && v4[1] == 0 && v4[2] == 2:
			return false
		case v4[0] == 198 && v4[1] == 51 && v4[2] == 100:
			return false
		case v4[0] == 203 && v4[1] == 0 && v4[2] == 113:
			return false
		case v4[0] >= 240:
			return false
		}
		return true
	}
	// IPv6 documentation prefix 2001:db8::/32.
	if len(ip) == net.IPv6len && ip[0] == 0x20 && ip[1] == 0x01 && ip[2] == 0x0d && ip[3] == 0xb8 {
		return false
	}
	return true
}

// Fetch downloads an HTTPS calendar. Redirects are re-checked. Errors are
// sentinels and do not include the URL.
func Fetch(ctx context.Context, raw string) ([]byte, error) {
	if err := CheckURL(ctx, raw, nil); err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		Proxy:               nil,
		TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout:     10 * time.Second,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, ErrBlocked
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil || len(ips) == 0 {
				return nil, ErrFetch
			}
			var last error = ErrBlocked
			for _, ip := range ips {
				if !AddressAllowed(ip.IP) {
					last = ErrBlocked
					continue
				}
				conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
				if err == nil {
					return conn, nil
				}
				last = ErrFetch
			}
			return nil, last
		},
	}
	client := &http.Client{
		Timeout:   20 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return ErrFetch
			}
			if err := CheckURL(req.Context(), req.URL.String(), nil); err != nil {
				return err
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(raw), nil)
	if err != nil {
		return nil, ErrURL
	}
	req.Header.Set("User-Agent", "TansuCalendar")
	req.Header.Set("Accept", "text/calendar, text/plain;q=0.9, */*;q=0.1")
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, ErrBlocked) || errors.Is(err, ErrURL) || errors.Is(err, ErrFetch) {
			return nil, err
		}
		return nil, ErrFetch
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, ErrFetch
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, ErrFetch
	}
	if len(body) > maxBody {
		return nil, ErrLarge
	}
	return body, nil
}
