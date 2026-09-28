package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/aquasp/kuraspend/internal/config"
	"github.com/aquasp/kuraspend/internal/store"
)

func TestPushPaymentDayReachesCalendar(t *testing.T) {
	var bodies []string
	var secret, path string
	cal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(raw))
		secret = r.Header.Get("X-Kura-Sync")
		path = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer cal.Close()
	f := newFlow(t, func(c *config.Config) {
		c.CalendarURL = cal.URL
		c.SyncSecret = "sync-secret"
	})
	u := f.seedUser("ada@example.com", "secret-password")
	if err := f.store.SetUserSub(u.ID, "acct-ada"); err != nil {
		t.Fatal(err)
	}
	_, raw, _ := f.store.CreateToken(u.ID, "hermes")

	code, out := f.apiCall(http.MethodPost, "/api/v1/payment_days", raw,
		`{"payment_day":{"title":"Water","due_day":10,"notes":"bill"}}`)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %+v", code, out)
	}
	if secret != "sync-secret" || path != "/sync/payment_days" {
		t.Fatalf("push %s %q", path, secret)
	}
	if len(bodies) != 1 {
		t.Fatalf("pushes: %d", len(bodies))
	}
	for _, want := range []string{`"source_key":"spend:`, `"title":"Water"`, `"due_day":10`, `"notes":"bill"`, `"account_sub":"acct-ada"`, `"delete":false`} {
		if !strings.Contains(bodies[0], want) {
			t.Fatalf("body %s missing %s", bodies[0], want)
		}
	}

	idStr := strconv.FormatFloat(out["payment_day"].(map[string]any)["id"].(float64), 'f', 0, 64)
	code, out = f.apiCall(http.MethodPatch, "/api/v1/payment_days/"+idStr, raw, `{"payment_day":{"active":false}}`)
	if code != http.StatusOK || out["payment_day"].(map[string]any)["active"] != false {
		t.Fatalf("deactivate: %d %+v", code, out)
	}
	if !strings.Contains(bodies[len(bodies)-1], `"delete":true`) {
		t.Fatalf("deactivate body %s", bodies[len(bodies)-1])
	}

	code, out = f.apiCall(http.MethodPatch, "/api/v1/payment_days/"+idStr, raw, `{"payment_day":{"active":true,"due_day":12}}`)
	if code != http.StatusOK {
		t.Fatalf("reactivate: %d %+v", code, out)
	}
	if !strings.Contains(bodies[len(bodies)-1], `"delete":false`) || !strings.Contains(bodies[len(bodies)-1], `"due_day":12`) {
		t.Fatalf("reactivate body %s", bodies[len(bodies)-1])
	}

	before := len(bodies)
	code, _ = f.apiCall(http.MethodPost, "/api/v1/payment_days", raw, `{"payment_day":{"title":""}}`)
	if code != http.StatusUnprocessableEntity || len(bodies) != before {
		t.Fatalf("invalid pushed: %d bodies %d -> %d", code, before, len(bodies))
	}

	code, _ = f.apiCall(http.MethodDelete, "/api/v1/payment_days/"+idStr, raw, "")
	if code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if !strings.Contains(bodies[len(bodies)-1], `"delete":true`) {
		t.Fatalf("delete body %s", bodies[len(bodies)-1])
	}

	before = len(bodies)
	code, out = f.apiCall(http.MethodPost, "/api/v1/subscriptions", raw,
		`{"subscription":{"title":"Music","amount":"19.90","due_day":10,"billing_month":3}}`)
	if code != http.StatusCreated || len(bodies) != before {
		t.Fatalf("subscription pushed: %d bodies %d -> %d", code, before, len(bodies))
	}
	sub := out["subscription"].(map[string]any)
	if _, ok := sub["due_day"]; ok {
		t.Fatalf("due_day still in API: %+v", sub)
	}
	if _, ok := sub["billing_month"]; ok {
		t.Fatalf("billing_month still in API: %+v", sub)
	}
	if sub["interval"] != "monthly" || sub["title"] != "Music" {
		t.Fatalf("subscription: %+v", sub)
	}
}

func TestPushPaymentDaySkippedWhenUnset(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	title, due := "Water", "10"
	day, fails, err := f.store.CreatePaymentDay(u.ID, store.PaymentDayPatch{
		Title: &title, DueDay: &due,
	})
	if err != nil || len(fails) > 0 {
		t.Fatalf("day: %v %v", fails, err)
	}
	f.srv.pushPaymentDay(u, day, false)
	f.srv.pushPaymentDay(nil, day, false)
}

func TestWebPaymentDayPushes(t *testing.T) {
	var bodies []string
	cal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(raw))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer cal.Close()
	f := newFlow(t, func(c *config.Config) {
		c.CalendarURL = cal.URL
		c.SyncSecret = "sync-secret"
	})
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	code, _, _ := f.post("/payment_days", url.Values{
		"payment_day[title]": {"Rent"}, "payment_day[due_day]": {"5"},
	}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("create: %d", code)
	}
	if len(bodies) != 1 || !strings.Contains(bodies[0], `"title":"Rent"`) || !strings.Contains(bodies[0], `"delete":false`) {
		t.Fatalf("web push: %+v", bodies)
	}
}
