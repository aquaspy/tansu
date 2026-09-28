package suite

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMailSendWaitsForConfirm(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path == "/api/v1/accounts/4/preview" {
			if strings.Contains(string(body), "secret-body") {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"preview":{"to":["ada@example.com"],"subject":"Friday","body":"secret-body"},"sent":false}`))
				return
			}
		}
		if r.URL.Path == "/api/v1/accounts/4/send" && r.Method == http.MethodPost {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"sent":true,"subject":"Friday"}`))
			return
		}
		if r.URL.Path == "/api/v1/accounts" {
			_, _ = w.Write([]byte(`{"accounts":[{"id":4,"display_name":"Me","from_address":"me@example.com","password":"nope"}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := &Client{App: App{Name: "email", Base: srv.URL}, Token: "kura_test"}
	args := map[string]any{
		"account_id": float64(4),
		"to":         []any{"ada@example.com"},
		"subject":    "Friday",
		"body":       "secret-body",
	}
	held := Execute(context.Background(), c, MailSend, args, false)
	if held.OK || !strings.Contains(held.Body, "needs_confirm") || hits != 0 {
		t.Fatalf("held %+v hits %d", held, hits)
	}
	preview := Execute(context.Background(), c, MailCompose, args, false)
	if !preview.OK || !strings.Contains(preview.Body, "secret-body") || strings.Contains(preview.Body, `"sent":true`) {
		t.Fatalf("preview %s", preview.Body)
	}
	sent := Execute(context.Background(), c, MailSend, args, true)
	if !sent.OK || !strings.Contains(sent.Body, `"sent":true`) || sent.App != "email" {
		t.Fatalf("send %+v", sent)
	}
	list := Execute(context.Background(), c, MailAccounts, nil, false)
	if !list.OK || strings.Contains(list.Body, "nope") || !strings.Contains(list.Body, "Me") {
		t.Fatalf("accounts %s", list.Body)
	}
	if !NeedsConfirm(MailSend) || !NeedsConfirm(MailDelete) || IsDelete(MailSend) || !IsDelete(MailTrash) {
		t.Fatal("confirm flags")
	}
	if text := SendPreviewText(args); !strings.Contains(text, "ada@example.com") || !strings.Contains(text, "Friday") || !strings.Contains(text, "secret-body") {
		t.Fatalf("preview text %q", text)
	}
	names := ToolsFor([]string{"email"})
	if len(names) != 9 {
		t.Fatalf("tools %d", len(names))
	}
}

func TestSuiteAppMailDoesNotHitSpend(t *testing.T) {
	if appOf(MailList) != "email" || appOf(SpendList) != "spend" || appOf("unknown") != "" {
		t.Fatalf("appOf mail %s spend %s unknown %s", appOf(MailList), appOf(SpendList), appOf("unknown"))
	}
	if ProbePath("email") != "/api/v1/accounts" {
		t.Fatal(ProbePath("email"))
	}
}
