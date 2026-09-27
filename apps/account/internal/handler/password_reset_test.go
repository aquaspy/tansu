package handler

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/aquasp/kuraaccount/internal/config"
	"golang.org/x/crypto/bcrypt"
)

func TestForgotHiddenWithoutMail(t *testing.T) {
	f := newFlow(t, nil)
	code, body, _ := f.get("/login", nil)
	if code != http.StatusOK {
		t.Fatalf("login: %d", code)
	}
	if strings.Contains(body, "Forgot password?") || strings.Contains(body, "/password/forgot") {
		t.Fatalf("forgot link without mail:\n%s", body)
	}
	code, _, h := f.get("/password/forgot", nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/login" {
		t.Fatalf("forgot: %d %q", code, h.Get("Location"))
	}
	code, _, h = f.post("/password/forgot", url.Values{"email": {"ada@example.com"}}, nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/login" {
		t.Fatalf("forgot post: %d %q", code, h.Get("Location"))
	}
	code, _, h = f.get("/password/reset?token=abc", nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/login" {
		t.Fatalf("reset: %d %q", code, h.Get("Location"))
	}
}

func TestForgotUnknownEmailLooksSent(t *testing.T) {
	f := newFlow(t, nil)
	box := f.useMail()
	code, body, _ := f.get("/login", nil)
	if code != http.StatusOK {
		t.Fatalf("login: %d", code)
	}
	mustContain(t, body, "Forgot password?")
	code, body, _ = f.post("/password/forgot", url.Values{"email": {"missing@example.com"}}, nil)
	if code != http.StatusOK {
		t.Fatalf("forgot: %d %s", code, body)
	}
	mustContain(t, body, "If an account exists for missing@example.com")
	if len(box.msgs) != 0 {
		t.Fatalf("sent %d messages", len(box.msgs))
	}
	var n int
	if err := f.store.DB().QueryRow(`SELECT COUNT(*) FROM password_resets`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("reset rows = %d", n)
	}
}

func TestForgotResetsPasswordAndDropsSessions(t *testing.T) {
	f := newFlow(t, nil)
	box := f.useMail()
	f.seedUser("ada@example.com", "password1")
	f.login("ada@example.com", "password1")
	if code, _, _ := f.get("/", nil); code != http.StatusOK {
		t.Fatalf("hub before reset: %d", code)
	}

	code, body, _ := f.post("/password/forgot", url.Values{"email": {"Ada@Example.com"}}, map[string]string{
		"Accept-Language": "pt-BR",
	})
	if code != http.StatusOK {
		t.Fatalf("forgot: %d %s", code, body)
	}
	mustContain(t, body, "ada@example.com")
	if len(box.msgs) != 1 || box.msgs[0].to != "ada@example.com" || box.msgs[0].subject != "Redefina sua senha Tansu" {
		t.Fatalf("mail: %+v", box.msgs)
	}
	link := tokenLink(t, box.msgs[0].text)
	if !strings.Contains(link, "/password/reset?token=") {
		t.Fatalf("link: %s", link)
	}
	token := tokenFromMail(t, box.msgs[0].text)

	code, body, _ = f.get("/password/reset?token="+url.QueryEscape(token), nil)
	if code != http.StatusOK {
		t.Fatalf("reset page: %d %s", code, body)
	}
	mustContain(t, body, "Escolha uma senha nova")

	code, body, _ = f.post("/password/reset", url.Values{
		"token": {token}, "password": {"short"}, "password_confirmation": {"short"},
	}, nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("short: %d %s", code, body)
	}
	if code, _, _ := f.get("/", nil); code != http.StatusOK {
		t.Fatalf("hub still open after rejected reset: %d", code)
	}

	code, _, h := f.post("/password/reset", url.Values{
		"token": {token}, "password": {"password2"}, "password_confirmation": {"password2"},
	}, nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/login?reset=1" {
		t.Fatalf("reset: %d %q", code, h.Get("Location"))
	}
	code, _, _ = f.get("/", nil)
	if code != http.StatusSeeOther {
		t.Fatalf("hub after reset: %d", code)
	}
	code, body, _ = f.get("/login?reset=1", nil)
	if code != http.StatusOK {
		t.Fatalf("login notice: %d %s", code, body)
	}
	mustContain(t, body, "Password changed")

	code, _, _ = f.post("/login", url.Values{"email": {"ada@example.com"}, "password": {"password1"}}, nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("old password: %d", code)
	}
	f.login("ada@example.com", "password2")

	code, body, _ = f.post("/password/reset", url.Values{
		"token": {token}, "password": {"password3"}, "password_confirmation": {"password3"},
	}, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("replay: %d %s", code, body)
	}
	user, err := f.store.FindUserByEmail("ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordDigest), []byte("password2")) != nil {
		t.Fatal("replay changed the password")
	}
}

func TestForgotExpiredToken(t *testing.T) {
	f := newFlow(t, nil)
	box := f.useMail()
	f.seedUser("ada@example.com", "password1")
	if code, body, _ := f.post("/password/forgot", url.Values{"email": {"ada@example.com"}}, nil); code != http.StatusOK {
		t.Fatalf("forgot: %d %s", code, body)
	}
	token := tokenFromMail(t, box.msgs[0].text)
	if _, err := f.store.DB().Exec(`UPDATE password_resets SET expires_at = ?`, "1999-01-01 00:00:00.000000"); err != nil {
		t.Fatal(err)
	}
	code, body, _ := f.get("/password/reset?token="+url.QueryEscape(token), nil)
	if code != http.StatusBadRequest {
		t.Fatalf("expired page: %d %s", code, body)
	}
	mustContain(t, body, "invalid or expired")
	code, _, _ = f.post("/password/reset", url.Values{
		"token": {token}, "password": {"password2"}, "password_confirmation": {"password2"},
	}, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("expired post: %d", code)
	}
	user, err := f.store.FindUserByEmail("ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordDigest), []byte("password1")) != nil {
		t.Fatal("expired link changed the password")
	}
}

func TestForgotRateLimit(t *testing.T) {
	f := newFlow(t, nil)
	box := f.useMail()
	f.seedUser("ada@example.com", "password1")
	form := url.Values{"email": {"ada@example.com"}}
	for i := 0; i < 3; i++ {
		code, body, _ := f.post("/password/forgot", form, nil)
		if code != http.StatusOK {
			t.Fatalf("request %d: %d %s", i, code, body)
		}
	}
	code, body, _ := f.post("/password/forgot", form, nil)
	if code != http.StatusTooManyRequests {
		t.Fatalf("limit: %d %s", code, body)
	}
	mustContain(t, body, "Too many attempts")
	if len(box.msgs) != 3 {
		t.Fatalf("sent %d messages", len(box.msgs))
	}
	// A different address is still limited the same way, so the 429
	// does not reveal which inbox exists.
	code, body, _ = f.post("/password/forgot", url.Values{"email": {"missing@example.com"}}, nil)
	if code != http.StatusOK {
		t.Fatalf("unknown after other limit: %d %s", code, body)
	}
	for i := 0; i < 2; i++ {
		if code, body, _ := f.post("/password/forgot", url.Values{"email": {"missing@example.com"}}, nil); code != http.StatusOK {
			t.Fatalf("unknown %d: %d %s", i, code, body)
		}
	}
	code, body, _ = f.post("/password/forgot", url.Values{"email": {"missing@example.com"}}, nil)
	if code != http.StatusTooManyRequests {
		t.Fatalf("unknown limit: %d %s", code, body)
	}
	if len(box.msgs) != 3 {
		t.Fatalf("unknown address sent mail: %+v", box.msgs)
	}
}

func TestForgotSendFailureKeepsPreviousLink(t *testing.T) {
	f := newFlow(t, nil)
	box := f.useMail()
	f.seedUser("ada@example.com", "password1")
	if code, body, _ := f.post("/password/forgot", url.Values{"email": {"ada@example.com"}}, nil); code != http.StatusOK {
		t.Fatalf("first: %d %s", code, body)
	}
	first := tokenFromMail(t, box.msgs[0].text)
	box.err = errors.New("resend down")
	code, body, _ := f.post("/password/forgot", url.Values{"email": {"ada@example.com"}}, nil)
	if code != http.StatusBadGateway {
		t.Fatalf("second: %d %s", code, body)
	}
	mustContain(t, body, "Could not send the reset email")
	code, _, h := f.post("/password/reset", url.Values{
		"token": {first}, "password": {"password2"}, "password_confirmation": {"password2"},
	}, nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/login?reset=1" {
		t.Fatalf("old link: %d %q", code, h.Get("Location"))
	}
}

func TestResetLinkUsesConfiguredHost(t *testing.T) {
	f := newFlow(t, func(c *config.Config) {
		c.KuraHosts = []string{"account.example.com"}
		c.ForceSSL = true
	})
	box := f.useMail()
	f.seedUser("ada@example.com", "password1")
	if code, body, _ := f.post("/password/forgot", url.Values{"email": {"ada@example.com"}}, nil); code != http.StatusOK {
		t.Fatalf("forgot: %d %s", code, body)
	}
	link := tokenLink(t, box.msgs[0].text)
	if !strings.HasPrefix(link, "https://account.example.com/password/reset?") {
		t.Fatalf("link: %s", link)
	}
}

func TestForgotMismatchDoesNotConsume(t *testing.T) {
	f := newFlow(t, nil)
	box := f.useMail()
	f.seedUser("ada@example.com", "password1")
	if code, body, _ := f.post("/password/forgot", url.Values{"email": {"ada@example.com"}}, nil); code != http.StatusOK {
		t.Fatalf("forgot: %d %s", code, body)
	}
	token := tokenFromMail(t, box.msgs[0].text)
	code, body, _ := f.post("/password/reset", url.Values{
		"token": {token}, "password": {"password2"}, "password_confirmation": {"different"},
	}, nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("mismatch: %d %s", code, body)
	}
	user, err := f.store.FindUserByEmail("ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordDigest), []byte("password1")) != nil {
		t.Fatal("mismatch changed the password")
	}
	if _, err := f.store.FindPasswordResetByToken(token); err != nil {
		t.Fatal(err)
	}
}
