package handler

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/aquasp/kuraaccount/internal/config"
	"github.com/aquasp/kuraaccount/internal/store"
)

type sentMail struct {
	to, subject, html, text string
}

type fakeMail struct {
	mu   sync.Mutex
	msgs []sentMail
	err  error
}

func (f *fakeMail) Send(_ context.Context, to, subject, html, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.msgs = append(f.msgs, sentMail{to: to, subject: subject, html: html, text: text})
	return nil
}

func (f *flow) useMail() *fakeMail {
	f.srv.Config.ResendAPIKey = "test-key"
	f.srv.Config.ResendFrom = "tansu@example.com"
	box := &fakeMail{}
	f.srv.Mail = box
	return box
}

func signupForm(email, password, next string) url.Values {
	v := url.Values{
		"email":                 {email},
		"password":              {password},
		"password_confirmation": {password},
	}
	if next != "" {
		v.Set("next", next)
	}
	return v
}

func tokenFromMail(t *testing.T, text string) string {
	t.Helper()
	start := strings.Index(text, "http")
	if start < 0 {
		t.Fatalf("no link in %q", text)
	}
	raw := strings.Fields(text[start:])[0]
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	token := u.Query().Get("token")
	if token == "" {
		t.Fatalf("link %q has no token", raw)
	}
	return token
}

func TestSignupCreatesUserWithoutMail(t *testing.T) {
	f := newFlow(t, nil)
	code, _, h := f.post("/signup", signupForm("ada@example.com", "password1", ""), nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/" {
		t.Fatalf("signup: %d %q", code, h.Get("Location"))
	}
	if _, err := f.store.FindUserByEmail("ada@example.com"); err != nil {
		t.Fatal(err)
	}
}

func TestSignupWaitsForConfirmation(t *testing.T) {
	f := newFlow(t, nil)
	box := f.useMail()
	code, body, _ := f.post("/signup", signupForm("Ada@Example.com", "password1", ""), map[string]string{
		"Accept-Language": "pt-BR",
	})
	if code != http.StatusOK {
		t.Fatalf("signup: %d %s", code, body)
	}
	mustContain(t, body, "Veja seu email")
	mustContain(t, body, "ada@example.com")
	if _, err := f.store.FindUserByEmail("ada@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("user existed before confirm: %v", err)
	}
	if len(box.msgs) != 1 || box.msgs[0].to != "ada@example.com" || box.msgs[0].subject != "Confirme sua conta Tansu" {
		t.Fatalf("mail: %+v", box.msgs)
	}
	token := tokenFromMail(t, box.msgs[0].text)

	code, body, _ = f.get("/signup/confirm?token="+url.QueryEscape(token), nil)
	if code != http.StatusOK {
		t.Fatalf("confirm page: %d %s", code, body)
	}
	mustContain(t, body, "Confirme sua conta")
	if _, err := f.store.FindUserByEmail("ada@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("GET confirm created a user")
	}

	code, body, _ = f.post("/signup/confirm", url.Values{"token": {token}, "password": {"nope-nope"}}, nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("bad password: %d %s", code, body)
	}
	if _, err := f.store.FindUserByEmail("ada@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("wrong password created a user")
	}

	code, _, h := f.post("/signup/confirm", url.Values{"token": {token}, "password": {"password1"}}, nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/" {
		t.Fatalf("confirm: %d %q", code, h.Get("Location"))
	}
	if _, err := f.store.FindUserByEmail("ada@example.com"); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := f.get("/", nil); code != http.StatusOK {
		t.Fatalf("hub after confirm: %d", code)
	}
	code, body, _ = f.post("/signup/confirm", url.Values{"token": {token}, "password": {"password1"}}, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("replay: %d %s", code, body)
	}
	users, err := f.store.ListUsers()
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 {
		t.Fatalf("users = %d", len(users))
	}
}

func TestSignupReplaceInvalidatesOldToken(t *testing.T) {
	f := newFlow(t, nil)
	box := f.useMail()
	if code, body, _ := f.post("/signup", signupForm("ada@example.com", "password1", ""), nil); code != http.StatusOK {
		t.Fatalf("first: %d %s", code, body)
	}
	first := tokenFromMail(t, box.msgs[0].text)
	if code, body, _ := f.post("/signup", signupForm("ada@example.com", "password2", ""), nil); code != http.StatusOK {
		t.Fatalf("second: %d %s", code, body)
	}
	second := tokenFromMail(t, box.msgs[1].text)
	code, _, _ := f.post("/signup/confirm", url.Values{"token": {first}, "password": {"password1"}}, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("old token: %d", code)
	}
	if _, err := f.store.FindUserByEmail("ada@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("old token created a user")
	}
	code, _, h := f.post("/signup/confirm", url.Values{"token": {second}, "password": {"password2"}}, nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/" {
		t.Fatalf("new token: %d %q", code, h.Get("Location"))
	}
}

func TestSignupTakenDoesNotSend(t *testing.T) {
	f := newFlow(t, nil)
	f.seedUser("ada@example.com", "password1")
	box := f.useMail()
	code, body, _ := f.post("/signup", signupForm("ada@example.com", "password2", ""), nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("signup: %d %s", code, body)
	}
	mustContain(t, body, "already registered")
	if len(box.msgs) != 0 {
		t.Fatalf("sent %d messages", len(box.msgs))
	}
}

func TestSignupSendFailureLeavesNoPending(t *testing.T) {
	f := newFlow(t, nil)
	box := f.useMail()
	box.err = errors.New("resend down")
	code, body, _ := f.post("/signup", signupForm("ada@example.com", "password1", ""), nil)
	if code != http.StatusBadGateway {
		t.Fatalf("signup: %d %s", code, body)
	}
	mustContain(t, body, "Could not send")
	if _, err := f.store.FindUserByEmail("ada@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	var n int
	if err := f.store.DB().QueryRow(`SELECT COUNT(*) FROM pending_signups`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("pending rows = %d", n)
	}
}

func TestSignupConfirmExpired(t *testing.T) {
	f := newFlow(t, nil)
	box := f.useMail()
	if code, body, _ := f.post("/signup", signupForm("ada@example.com", "password1", ""), nil); code != http.StatusOK {
		t.Fatalf("signup: %d %s", code, body)
	}
	token := tokenFromMail(t, box.msgs[0].text)
	if _, err := f.store.DB().Exec(`UPDATE pending_signups SET expires_at = ?`, "1999-01-01 00:00:00.000000"); err != nil {
		t.Fatal(err)
	}
	code, body, _ := f.get("/signup/confirm?token="+url.QueryEscape(token), nil)
	if code != http.StatusBadRequest {
		t.Fatalf("expired page: %d %s", code, body)
	}
	mustContain(t, body, "invalid or expired")
	if _, err := f.store.FindUserByEmail("ada@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("expired link created a user")
	}
}

func TestSignupConfirmReturnsToAuthorize(t *testing.T) {
	f := newFlow(t, nil)
	box := f.useMail()
	next := authorizePath("back")
	if code, body, _ := f.post("/signup", signupForm("ada@example.com", "password1", next), nil); code != http.StatusOK {
		t.Fatalf("signup: %d %s", code, body)
	}
	token := tokenFromMail(t, box.msgs[0].text)
	code, _, h := f.post("/signup/confirm", url.Values{"token": {token}, "password": {"password1"}}, nil)
	if code != http.StatusSeeOther || h.Get("Location") != next {
		t.Fatalf("confirm: %d %q want %q", code, h.Get("Location"), next)
	}
}

func TestSignupDropsForeignNextInMail(t *testing.T) {
	f := newFlow(t, nil)
	box := f.useMail()
	if code, body, _ := f.post("/signup", signupForm("ada@example.com", "password1", "https://evil.example/authorize"), nil); code != http.StatusOK {
		t.Fatalf("signup: %d %s", code, body)
	}
	token := tokenFromMail(t, box.msgs[0].text)
	code, _, h := f.post("/signup/confirm", url.Values{"token": {token}, "password": {"password1"}}, nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/" {
		t.Fatalf("confirm: %d %q", code, h.Get("Location"))
	}
}

func TestConfirmLinkUsesHTTPS(t *testing.T) {
	f := newFlow(t, func(c *config.Config) { c.ForceSSL = true })
	box := f.useMail()
	if code, body, _ := f.post("/signup", signupForm("ada@example.com", "password1", ""), nil); code != http.StatusOK {
		t.Fatalf("signup: %d %s", code, body)
	}
	if !strings.HasPrefix(tokenLink(t, box.msgs[0].text), "https://") {
		t.Fatalf("forced link: %s", box.msgs[0].text)
	}

	f = newFlow(t, nil)
	box = f.useMail()
	if code, body, _ := f.post("/signup", signupForm("bea@example.com", "password1", ""), map[string]string{
		"X-Forwarded-Proto": "https",
	}); code != http.StatusOK {
		t.Fatalf("forwarded signup: %d %s", code, body)
	}
	if !strings.HasPrefix(tokenLink(t, box.msgs[0].text), "https://") {
		t.Fatalf("forwarded link: %s", box.msgs[0].text)
	}
}

func tokenLink(t *testing.T, text string) string {
	t.Helper()
	start := strings.Index(text, "http")
	if start < 0 {
		t.Fatalf("no link in %q", text)
	}
	return strings.Fields(text[start:])[0]
}

func TestFailedResendKeepsPreviousLink(t *testing.T) {
	f := newFlow(t, nil)
	box := f.useMail()
	if code, body, _ := f.post("/signup", signupForm("ada@example.com", "password1", ""), nil); code != http.StatusOK {
		t.Fatalf("first: %d %s", code, body)
	}
	first := tokenFromMail(t, box.msgs[0].text)
	box.err = errors.New("resend down")
	code, body, _ := f.post("/signup", signupForm("ada@example.com", "password2", ""), nil)
	if code != http.StatusBadGateway {
		t.Fatalf("second: %d %s", code, body)
	}
	code, _, h := f.post("/signup/confirm", url.Values{"token": {first}, "password": {"password1"}}, nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/" {
		t.Fatalf("old link: %d %q", code, h.Get("Location"))
	}
}

func TestConfirmBurnsAfterPasswordFailures(t *testing.T) {
	f := newFlow(t, nil)
	box := f.useMail()
	if code, body, _ := f.post("/signup", signupForm("ada@example.com", "password1", ""), nil); code != http.StatusOK {
		t.Fatalf("signup: %d %s", code, body)
	}
	token := tokenFromMail(t, box.msgs[0].text)
	if _, err := f.store.DB().Exec(`UPDATE pending_signups SET attempts = ?`, store.ConfirmAttemptLimit-1); err != nil {
		t.Fatal(err)
	}
	code, body, _ := f.post("/signup/confirm", url.Values{"token": {token}, "password": {"nope-nope"}}, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("burn: %d %s", code, body)
	}
	mustContain(t, body, "Request a new confirmation email")
	code, _, _ = f.post("/signup/confirm", url.Values{"token": {token}, "password": {"password1"}}, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("token still live: %d", code)
	}
	if _, err := f.store.FindUserByEmail("ada@example.com"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("burned link created a user")
	}
}

func TestConfirmLinkUsesConfiguredHost(t *testing.T) {
	f := newFlow(t, func(c *config.Config) {
		c.KuraHosts = []string{"account.example.com", "account.example.org"}
		c.ForceSSL = true
	})
	box := f.useMail()
	if code, body, _ := f.post("/signup", signupForm("ada@example.com", "password1", ""), nil); code != http.StatusOK {
		t.Fatalf("signup: %d %s", code, body)
	}
	link := tokenLink(t, box.msgs[0].text)
	if !strings.HasPrefix(link, "https://account.example.com/signup/confirm?") {
		t.Fatalf("link: %s", link)
	}
}

func TestConfirmWithoutMailRedirects(t *testing.T) {
	f := newFlow(t, nil)
	code, _, h := f.get("/signup/confirm?token=abc", nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/signup" {
		t.Fatalf("confirm: %d %q", code, h.Get("Location"))
	}
}
