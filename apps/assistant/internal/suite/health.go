package suite

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Health states for one sibling app at probe time.
const (
	HealthOK           = "ok"
	HealthAuth         = "auth"
	HealthDown         = "down"
	HealthOff          = "off"
	HealthBroken       = "broken"
	HealthUnconfigured = "unconfigured"
)

// AppOrder is the product order: People, Spend, Calendar, Notes, Email.
var AppOrder = []string{"people", "spend", "calendar", "notes", "email"}

// Health is one app's runtime connection.
type Health struct {
	App    string
	State  string
	Detail string // last failure, empty when connected
}

// ProbePath is a cheap authenticated read used to tell auth from reachability.
func ProbePath(app string) string {
	switch app {
	case "notes":
		return "/api/v1/folders"
	case "people":
		return "/api/v1/people?limit=1"
	case "calendar":
		return "/api/v1/events?from=2000-01-01&to=2000-01-01"
	case "spend":
		return "/api/v1/subscriptions"
	case "email":
		return "/api/v1/accounts"
	default:
		return ""
	}
}

// Probe calls the app once. A 2xx is healthy; 401/403 is an auth failure;
// anything else, including a timeout, is down. The caller decides off,
// broken, and unconfigured before probing.
func Probe(ctx context.Context, c Client) Health {
	path := ProbePath(c.App.Name)
	out := Health{App: c.App.Name, State: HealthDown, Detail: "unreachable"}
	if path == "" || c.App.Base == "" {
		out.State = HealthUnconfigured
		out.Detail = ""
		return out
	}
	if c.HTTP == nil {
		c.HTTP = &http.Client{
			Timeout: 1500 * time.Millisecond,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
	}
	pctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	code, _, err := c.Call(pctx, http.MethodGet, path, nil)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || Lost(err) {
			out.Detail = "timeout"
			return out
		}
		out.Detail = "unreachable"
		return out
	}
	switch {
	case code >= 200 && code < 300:
		return Health{App: c.App.Name, State: HealthOK}
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return Health{App: c.App.Name, State: HealthAuth, Detail: "unauthorized"}
	default:
		return Health{App: c.App.Name, State: HealthDown, Detail: fmt.Sprintf("HTTP %d", code)}
	}
}
