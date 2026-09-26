// Package mail sends the signup confirmation. A nil sender means the
// Account is running without Resend and creates users immediately.
package mail

import (
	"context"

	"github.com/resend/resend-go/v4"
)

// Sender delivers one transactional message.
type Sender interface {
	Send(ctx context.Context, to, subject, html, text string) error
}

// Resend talks to the Resend API.
type Resend struct {
	client *resend.Client
	from   string
}

// NewResend builds a client. apiKey and from are the process env values.
func NewResend(apiKey, from string) *Resend {
	return &Resend{client: resend.NewClient(apiKey), from: from}
}

// Send delivers one confirmation message.
func (r *Resend) Send(ctx context.Context, to, subject, html, text string) error {
	_, err := r.client.Emails.SendWithContext(ctx, &resend.SendEmailRequest{
		From:    r.from,
		To:      []string{to},
		Subject: subject,
		Html:    html,
		Text:    text,
	})
	return err
}
