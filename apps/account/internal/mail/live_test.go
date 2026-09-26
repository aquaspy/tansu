package mail

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestResendLiveSend hits the real API. It stays skipped unless
// RESEND_LIVE=1 and RESEND_API_KEY are set in the environment, so the
// suite and CI never need a network or a secret.
func TestResendLiveSend(t *testing.T) {
	if os.Getenv("RESEND_LIVE") != "1" {
		t.Skip("set RESEND_LIVE=1 to send one real message")
	}
	key := os.Getenv("RESEND_API_KEY")
	if key == "" {
		t.Fatal("RESEND_API_KEY is empty")
	}
	from := os.Getenv("RESEND_FROM")
	if from == "" {
		from = "onboarding@resend.dev"
	}
	to := os.Getenv("RESEND_TO")
	if to == "" {
		t.Fatal("RESEND_TO is empty")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	err := NewResend(key, from).Send(ctx, to, "Tansu Account confirmation test",
		"<p>This is a Tansu Account delivery test. No account was created.</p>",
		"This is a Tansu Account delivery test. No account was created.\n")
	if err != nil {
		t.Fatal(err)
	}
}
