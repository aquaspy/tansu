package mail

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveMailcowSearch talks to a real Dovecot when TANSU_IMAP_PASSWORD is
// set. It is not run in CI. Do not commit a password.
func TestLiveMailcowSearch(t *testing.T) {
	pw := os.Getenv("TANSU_IMAP_PASSWORD")
	user := os.Getenv("TANSU_IMAP_USER")
	if pw == "" || user == "" {
		t.Skip("set TANSU_IMAP_USER and TANSU_IMAP_PASSWORD to probe a live mailbox")
	}
	host := os.Getenv("TANSU_IMAP_HOST")
	if host == "" {
		host = "mailcow.sobremail.com"
	}
	c := Creds{
		IMAPHost: host, IMAPPort: 993, IMAPTLS: "tls",
		Username: user, Password: pw,
	}
	svc := NewService()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	page, err := svc.List(ctx, 1, c, "INBOX", "", 1)
	if err != nil || page.Total < 1000 || len(page.Messages) == 0 {
		t.Fatalf("list %+v %v", page, err)
	}
	t.Logf("list %s total %d", time.Since(start).Round(time.Millisecond), page.Total)

	start = time.Now()
	page, err = svc.List(ctx, 2, c, "INBOX", "grok", 1)
	if err != nil || page.Total < 1 || len(page.Messages) == 0 {
		t.Fatalf("header search %+v %v", page, err)
	}
	if time.Since(start) > 8*time.Second {
		t.Fatalf("header search took %s", time.Since(start))
	}
	t.Logf("grok %s total %d capped %v subject %q", time.Since(start).Round(time.Millisecond), page.Total, page.Capped, page.Messages[0].Subject)

	start = time.Now()
	page, err = svc.List(ctx, 4, c, "INBOX", "não", 1)
	if err != nil || page.Total < 1 || len(page.Messages) == 0 {
		t.Fatalf("utf-8 header search %+v %v", page, err)
	}
	if time.Since(start) > 8*time.Second {
		t.Fatalf("utf-8 header search took %s", time.Since(start))
	}
	t.Logf("não %s total %d capped %v", time.Since(start).Round(time.Millisecond), page.Total, page.Capped)

	start = time.Now()
	page, err = svc.List(ctx, 3, c, "INBOX", "text:grok", 1)
	if err != nil || page.Total < 1 || len(page.Messages) == 0 || !page.Capped {
		t.Fatalf("body search %+v %v", page, err)
	}
	if time.Since(start) > 8*time.Second {
		t.Fatalf("body search took %s", time.Since(start))
	}
	t.Logf("text:grok %s total %d capped %v subject %q", time.Since(start).Round(time.Millisecond), page.Total, page.Capped, page.Messages[0].Subject)
}
