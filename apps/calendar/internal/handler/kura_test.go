package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/aquasp/kuracalendar/internal/config"
	"golang.org/x/crypto/bcrypt"
)

// fakeAccount stands in for Kura Account: the token endpoint mints a
// fixed access token and userinfo returns the given profile for it.
func fakeAccount(t *testing.T, profile string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/token":
			_, _ = io.WriteString(w, `{"access_token":"fake-at","token_type":"bearer","expires_in":3600}`)
		case "/userinfo":
			if r.Header.Get("Authorization") != "Bearer fake-at" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, profile)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func withAccount(t *testing.T, profile string, mutate func(*config.Config)) (*flow, func()) {
	t.Helper()
	acct := fakeAccount(t, profile)
	f := newFlow(t, func(cfg *config.Config) {
		cfg.KuraAccountURL = acct.URL
		cfg.KuraClientID = "kuracalendar"
		cfg.KuraClientSecret = "test-secret"
		if mutate != nil {
			mutate(cfg)
		}
	})
	return f, acct.Close
}

func setKuraStateCookie(f *flow, state string) {
	u, _ := url.Parse(f.server.URL)
	f.client.Jar.SetCookies(u, []*http.Cookie{{Name: kuraStateCookie, Value: state, Path: "/"}})
}

func kuraCallback(t *testing.T, f *flow, state string) (int, string, http.Header) {
	t.Helper()
	if err := f.store.CreateKuraLogin(state, "test-verifier"); err != nil {
		t.Fatal(err)
	}
	setKuraStateCookie(f, state)
	return f.get("/login/kura/callback?code=test-code&state="+state, nil)
}

func TestKuraStartRedirectsToAccount(t *testing.T) {
	f, done := withAccount(t, `{}`, nil)
	defer done()
	code, _, hdr := f.get("/login/kura", nil)
	if code != http.StatusSeeOther {
		t.Fatalf("start status = %d, want 303", code)
	}
	loc := hdr.Get("Location")
	if !strings.HasPrefix(loc, f.srv.Config.KuraAccountURL+"/authorize?") {
		t.Fatalf("start redirects to %q, want account authorize", loc)
	}
	for _, param := range []string{"code_challenge=", "code_challenge_method=S256", "state="} {
		if !strings.Contains(loc, param) {
			t.Fatalf("authorize URL %q misses %s", loc, param)
		}
	}
	parsed, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	state := parsed.Query().Get("state")
	var bound string
	for _, line := range hdr.Values("Set-Cookie") {
		if strings.HasPrefix(line, kuraStateCookie+"=") {
			bound = line
		}
	}
	if state == "" || !strings.Contains(bound, kuraStateCookie+"="+state) ||
		!strings.Contains(bound, "HttpOnly") || !strings.Contains(bound, "SameSite=Lax") {
		t.Fatalf("state cookie %q does not bind authorize state %q", bound, state)
	}
}

func TestKuraCallbackProvisionsNewUser(t *testing.T) {
	f, done := withAccount(t, `{"sub":"acct-1","email":"sso@example.com","email_verified":true,"name":"SSO"}`, nil)
	defer done()
	code, _, hdr := kuraCallback(t, f, "state-provision")
	if code != http.StatusSeeOther || hdr.Get("Location") != "/" {
		t.Fatalf("callback status = %d loc %q, want 303 to /", code, hdr.Get("Location"))
	}
	user, err := f.store.FindUserByEmail("sso@example.com")
	if err != nil {
		t.Fatalf("provisioned user missing: %v", err)
	}
	if user.AccountSub != "acct-1" {
		t.Fatalf("AccountSub = %q, want acct-1", user.AccountSub)
	}
	if code, _, _ := f.get("/", nil); code != http.StatusOK {
		t.Fatalf("GET / after SSO = %d, want 200 (session cookie)", code)
	}
}

func TestKuraCallbackLinksExistingEmail(t *testing.T) {
	f, done := withAccount(t, `{"sub":"acct-2","email":"ana@example.com","email_verified":true}`, nil)
	defer done()
	digest, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	existing, err := f.store.CreateUser("ana@example.com", string(digest))
	if err != nil {
		t.Fatal(err)
	}
	code, _, _ := kuraCallback(t, f, "state-link")
	if code != http.StatusSeeOther {
		t.Fatalf("callback status = %d, want 303", code)
	}
	linked, err := f.store.FindUser(existing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if linked.AccountSub != "acct-2" {
		t.Fatalf("AccountSub = %q, want acct-2", linked.AccountSub)
	}
}

func TestKuraCallbackRejectsSubOverwrite(t *testing.T) {
	f, done := withAccount(t, `{"sub":"acct-new","email":"ana@example.com"}`, nil)
	defer done()
	digest, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	existing, err := f.store.CreateUser("ana@example.com", string(digest))
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetUserSub(existing.ID, "acct-old"); err != nil {
		t.Fatal(err)
	}
	code, _, _ := kuraCallback(t, f, "state-overwrite")
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", code)
	}
	linked, err := f.store.FindUser(existing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if linked.AccountSub != "acct-old" {
		t.Fatalf("AccountSub = %q, want acct-old", linked.AccountSub)
	}
}

func TestKuraCallbackRejectsUnknownState(t *testing.T) {
	f, done := withAccount(t, `{"sub":"acct-3","email":"x@example.com"}`, nil)
	defer done()
	code, body, _ := f.get("/login/kura/callback?code=test-code&state=nope", nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", code)
	}
	if !strings.Contains(body, "Tansu") {
		t.Fatalf("expected Tansu failure message, got: %.120s", body)
	}
	if _, err := f.store.FindUserByEmail("x@example.com"); err == nil {
		t.Fatal("unknown state must not provision a user")
	}
}

func TestKuraCallbackRejectsReplay(t *testing.T) {
	f, done := withAccount(t, `{"sub":"acct-4","email":"replay@example.com"}`, nil)
	defer done()
	if err := f.store.CreateKuraLogin("state-once", "test-verifier"); err != nil {
		t.Fatal(err)
	}
	setKuraStateCookie(f, "state-once")
	if code, _, _ := f.get("/login/kura/callback?code=test-code&state=state-once", nil); code != http.StatusSeeOther {
		t.Fatalf("first callback = %d, want 303", code)
	}
	if code, _, _ := f.get("/login/kura/callback?code=test-code&state=state-once", nil); code != http.StatusUnprocessableEntity {
		t.Fatalf("replayed callback = %d, want 422", code)
	}
}

func TestKuraCallbackRejectsMissingStateCookie(t *testing.T) {
	f, done := withAccount(t, `{"sub":"acct-6","email":"cookie@example.com"}`, nil)
	defer done()
	if err := f.store.CreateKuraLogin("state-nocookie", "test-verifier"); err != nil {
		t.Fatal(err)
	}
	code, _, _ := f.get("/login/kura/callback?code=test-code&state=state-nocookie", nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", code)
	}
	if _, err := f.store.FindUserByEmail("cookie@example.com"); err == nil {
		t.Fatal("missing state cookie must not provision a user")
	}
	// The pending login stays usable for the browser that holds the cookie.
	setKuraStateCookie(f, "state-nocookie")
	code, _, hdr := f.get("/login/kura/callback?code=test-code&state=state-nocookie", nil)
	if code != http.StatusSeeOther || hdr.Get("Location") != "/" {
		t.Fatalf("retry with cookie = %d loc %q, want 303 /", code, hdr.Get("Location"))
	}
}

func TestKuraCallbackRespectsClosedSignup(t *testing.T) {
	f, done := withAccount(t, `{"sub":"acct-5","email":"new@example.com"}`,
		func(cfg *config.Config) { cfg.SignupEnabled = false })
	defer done()
	code, body, _ := kuraCallback(t, f, "state-closed")
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", code)
	}
	if !strings.Contains(body, "turned off") && !strings.Contains(body, "desligados") {
		t.Fatalf("expected signup-closed message, got: %.160s", body)
	}
}

func TestKuraStartUsesExistingSession(t *testing.T) {
	f, done := withAccount(t, `{}`, nil)
	defer done()
	digest, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	if _, err := f.store.CreateUser("ada@example.com", string(digest)); err != nil {
		t.Fatal(err)
	}
	code, _, _ := f.methodCall(http.MethodPost, "/login", url.Values{
		"email": {"ada@example.com"}, "password": {"password123"},
	}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("login: %d", code)
	}
	code, _, hdr := f.get("/login/kura", nil)
	if code != http.StatusSeeOther || hdr.Get("Location") != "/" {
		t.Fatalf("start with session = %d loc %q, want 303 /", code, hdr.Get("Location"))
	}
}

func TestKuraButtonOnlyWhenConfigured(t *testing.T) {
	f, done := withAccount(t, `{}`, nil)
	defer done()
	if _, body, _ := f.get("/login", nil); !strings.Contains(body, "/login/kura") {
		t.Fatal("login page must offer Kura sign-in when configured")
	}

	plain := newFlow(t, nil) // no KURA_ACCOUNT_URL: standalone
	if code, _, _ := plain.get("/login/kura", nil); code != http.StatusSeeOther {
		t.Fatalf("unconfigured start = %d, want 303 to /login", code)
	}
	if _, body, _ := plain.get("/login", nil); strings.Contains(body, "/login/kura") {
		t.Fatal("standalone login page must not mention Kura sign-in")
	}
}

// TestStandaloneLoginUnaffected is the dual-mode regression: password
// login keeps working byte-for-byte with SSO enabled.
func TestStandaloneLoginUnaffected(t *testing.T) {
	f, done := withAccount(t, `{}`, nil)
	defer done()
	digest, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
	if _, err := f.store.CreateUser("local@example.com", string(digest)); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"email": {"local@example.com"}, "password": {"password123"}}
	code, _, hdr := f.methodCall(http.MethodPost, "/login", form, nil)
	if code != http.StatusSeeOther || hdr.Get("Location") != "/" {
		t.Fatalf("password login = %d loc %q, want 303 to /", code, hdr.Get("Location"))
	}
	if code, _, _ := f.get("/", nil); code != http.StatusOK {
		t.Fatalf("GET / after password login = %d, want 200", code)
	}
}
