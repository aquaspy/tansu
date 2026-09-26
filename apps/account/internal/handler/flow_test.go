package handler

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/aquasp/kuraaccount/internal/config"
	"github.com/aquasp/kuraaccount/internal/store"
	"golang.org/x/crypto/bcrypt"
)

const (
	testClientID     = "kurapeople"
	testClientSecret = "test-secret-16-chars"
	testRedirect     = "http://127.0.0.1:3005/login/kura/callback"
	testVerifier     = "test-verifier-1234567890abcdef"
)

func testChallenge() string {
	sum := sha256.Sum256([]byte(testVerifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type flow struct {
	t      *testing.T
	server *httptest.Server
	client *http.Client
	store  *store.Store
	srv    *Server
}

func newFlow(t *testing.T, mutate func(*config.Config)) *flow {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := config.Config{DataDir: t.TempDir(), SignupEnabled: true}
	if mutate != nil {
		mutate(&cfg)
	}
	if err := st.SeedClients([]store.SeedClient{{
		ID: testClientID, Secret: testClientSecret,
		Name: "TansuPeople", Home: "http://127.0.0.1:3005/", Icon: "🧑",
		RedirectURIs: []string{testRedirect},
	}}); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, st)
	srv.WebDir = "../../web/static"
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	f := &flow{t: t, server: ts, client: client, store: st, srv: srv}
	_, _, _ = f.get("/login", nil)
	return f
}

func (f *flow) csrf() string {
	u, _ := url.Parse(f.server.URL)
	for _, c := range f.client.Jar.Cookies(u) {
		if c.Name == CSRFCookie {
			return c.Value
		}
	}
	return ""
}

func (f *flow) get(path string, headers map[string]string) (int, string, http.Header) {
	req, _ := http.NewRequest(http.MethodGet, f.server.URL+path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body), resp.Header
}

func (f *flow) methodCall(method, path string, form url.Values, headers map[string]string) (int, string, http.Header) {
	if form == nil {
		form = url.Values{}
	}
	form.Set("csrf_token", f.csrf())
	req, _ := http.NewRequest(method, f.server.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", f.csrf())
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body), resp.Header
}

func (f *flow) post(path string, form url.Values, headers map[string]string) (int, string, http.Header) {
	return f.methodCall(http.MethodPost, path, form, headers)
}

func (f *flow) seedUser(email, password string) *store.User {
	digest, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		f.t.Fatal(err)
	}
	u, err := f.store.CreateUser(email, string(digest))
	if err != nil {
		f.t.Fatal(err)
	}
	return u
}

func (f *flow) login(email, password string) {
	code, _, _ := f.post("/login", url.Values{"email": {email}, "password": {password}}, nil)
	if code != http.StatusSeeOther {
		f.t.Fatalf("login status = %d", code)
	}
}

func mustContain(t *testing.T, body, want string) {
	t.Helper()
	if !strings.Contains(body, want) {
		t.Fatalf("body missing %q", want)
	}
}

func authorizePath(state string) string {
	v := url.Values{
		"client_id":             {testClientID},
		"redirect_uri":          {testRedirect},
		"response_type":         {"code"},
		"scope":                 {"openid email"},
		"state":                 {state},
		"code_challenge":        {testChallenge()},
		"code_challenge_method": {"S256"},
	}
	return "/authorize?" + v.Encode()
}

func (f *flow) tokenCall(form url.Values, id, secret string) (int, map[string]any) {
	req, _ := http.NewRequest(http.MethodPost, f.server.URL+"/token",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if id != "" {
		req.SetBasicAuth(id, secret)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func TestHubRequiresAuth(t *testing.T) {
	f := newFlow(t, nil)
	if code, _, h := f.get("/", nil); code != http.StatusSeeOther || h.Get("Location") != "/login" {
		t.Fatalf("hub: %d %q", code, h.Get("Location"))
	}
}

func TestHubShowsAppsAndDrawers(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	code, body, _ := f.get("/", nil)
	if code != http.StatusOK {
		t.Fatalf("hub: %d", code)
	}
	for _, want := range []string{"TansuPeople", "0 of 9 drawers", "Not connected yet", "Connect", "http://127.0.0.1:3005/login/kura"} {
		mustContain(t, body, want)
	}
	// Link the app through a redemption, drawers light up.
	raw, _ := f.store.CreateAuthCode(testClientID, u.ID, testRedirect, "c")
	if _, err := f.store.RedeemAuthCode(raw, testClientID, testRedirect); err != nil {
		t.Fatal(err)
	}
	_, body, _ = f.get("/", nil)
	for _, want := range []string{"1 of 9 drawers", "Connected", "Open", "http://127.0.0.1:3005/"} {
		mustContain(t, body, want)
	}
	if strings.Contains(body, "/login/kura") {
		t.Fatal("a connected app should open its home, not start SSO again")
	}
}

func TestOAuthHappyPath(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")

	code, _, h := f.get(authorizePath("xyz"), nil)
	if code != http.StatusSeeOther {
		t.Fatalf("authorize: %d", code)
	}
	dest, err := url.Parse(h.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h.Get("Location"), testRedirect) {
		t.Fatalf("redirect: %q", h.Get("Location"))
	}
	authCode := dest.Query().Get("code")
	if authCode == "" || dest.Query().Get("state") != "xyz" {
		t.Fatalf("params: %q", h.Get("Location"))
	}

	status, out := f.tokenCall(url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {authCode},
		"redirect_uri":  {testRedirect},
		"code_verifier": {testVerifier},
	}, testClientID, testClientSecret)
	if status != http.StatusOK {
		t.Fatalf("token: %d %+v", status, out)
	}
	access, _ := out["access_token"].(string)
	if access == "" || out["token_type"] != "Bearer" {
		t.Fatalf("token shape: %+v", out)
	}

	status, body, _ := f.get("/userinfo", map[string]string{"Authorization": "Bearer " + access})
	if status != http.StatusOK {
		t.Fatalf("userinfo: %d %s", status, body)
	}
	var info map[string]string
	_ = json.Unmarshal([]byte(body), &info)
	if info["email"] != u.Email || info["sub"] == "" {
		t.Fatalf("userinfo: %s", body)
	}
}

func TestAuthorizeRequiresLogin(t *testing.T) {
	f := newFlow(t, nil)
	code, _, h := f.get(authorizePath("xyz"), nil)
	if code != http.StatusSeeOther || !strings.HasPrefix(h.Get("Location"), "/login?next=") {
		t.Fatalf("authorize anonymous: %d %q", code, h.Get("Location"))
	}
}

func TestAuthorizeRejects(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	cases := map[string]string{
		"unknown client":  "/authorize?client_id=nope&redirect_uri=" + url.QueryEscape(testRedirect) + "&response_type=code&code_challenge=c&code_challenge_method=S256",
		"bad redirect":    "/authorize?client_id=" + testClientID + "&redirect_uri=http%3A%2F%2Fevil.example.com%2F&response_type=code&code_challenge=c&code_challenge_method=S256",
		"prefix redirect": "/authorize?client_id=" + testClientID + "&redirect_uri=" + url.QueryEscape(testRedirect+"/evil") + "&response_type=code&code_challenge=c&code_challenge_method=S256",
		"bad response":    "/authorize?client_id=" + testClientID + "&redirect_uri=" + url.QueryEscape(testRedirect) + "&response_type=token&code_challenge=c&code_challenge_method=S256",
		"missing PKCE":    "/authorize?client_id=" + testClientID + "&redirect_uri=" + url.QueryEscape(testRedirect) + "&response_type=code",
		"plain PKCE":      "/authorize?client_id=" + testClientID + "&redirect_uri=" + url.QueryEscape(testRedirect) + "&response_type=code&code_challenge=c&code_challenge_method=plain",
		"unknown scope":   "/authorize?client_id=" + testClientID + "&redirect_uri=" + url.QueryEscape(testRedirect) + "&response_type=code&scope=openid+admin&code_challenge=c&code_challenge_method=S256",
	}
	for name, path := range cases {
		if code, _, h := f.get(path, nil); code != http.StatusBadRequest {
			t.Errorf("%s: status = %d (%q), want 400", name, code, h.Get("Location"))
		}
	}
}

func TestTokenRejects(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")

	_, _, h := f.get(authorizePath("s"), nil)
	dest, _ := url.Parse(h.Get("Location"))
	good := dest.Query().Get("code")
	form := func(code, verifier string) url.Values {
		return url.Values{
			"grant_type": {"authorization_code"}, "code": {code},
			"redirect_uri": {testRedirect}, "code_verifier": {verifier},
		}
	}
	_ = u
	// Wrong PKCE burns the code without issuing.
	if status, _ := f.tokenCall(form(good, "wrong-verifier"), testClientID, testClientSecret); status != http.StatusBadRequest {
		t.Fatalf("bad PKCE: %d", status)
	}
	if status, _ := f.tokenCall(form(good, testVerifier), testClientID, testClientSecret); status != http.StatusBadRequest {
		t.Fatalf("burned code reused: %d", status)
	}
	// Bad secret never reaches the grant.
	_, _, h = f.get(authorizePath("s2"), nil)
	dest, _ = url.Parse(h.Get("Location"))
	if status, _ := f.tokenCall(form(dest.Query().Get("code"), testVerifier), testClientID, "wrong"); status != http.StatusUnauthorized {
		t.Fatalf("bad secret: %d", status)
	}
	// No basic auth at all.
	if status, _ := f.tokenCall(form("x", "y"), "", ""); status != http.StatusUnauthorized {
		t.Fatalf("no auth: %d", status)
	}
	// Bogus code.
	if status, out := f.tokenCall(form("bogus", testVerifier), testClientID, testClientSecret); status != http.StatusBadRequest || out["error"] != "invalid_grant" {
		t.Fatalf("bogus code: %d %+v", status, out)
	}
	// Userinfo rejects unknown tokens.
	if status, _, _ := f.get("/userinfo", map[string]string{"Authorization": "Bearer nope"}); status != http.StatusUnauthorized {
		t.Fatalf("userinfo bogus: %d", status)
	}
}

func TestSignupClosed(t *testing.T) {
	f := newFlow(t, func(c *config.Config) { c.SignupEnabled = false })
	if code, _, h := f.get("/signup", nil); code != http.StatusSeeOther || h.Get("Location") != "/login" {
		t.Fatalf("signup page: %d %q", code, h.Get("Location"))
	}
}

func TestLoginReturnsToAuthorize(t *testing.T) {
	f := newFlow(t, nil)
	f.seedUser("ada@example.com", "secret-password")
	next := authorizePath("back")
	code, _, h := f.post("/login", url.Values{
		"email": {"ada@example.com"}, "password": {"secret-password"}, "next": {next},
	}, nil)
	if code != http.StatusSeeOther || h.Get("Location") != next {
		t.Fatalf("login next: %d %q", code, h.Get("Location"))
	}
}

func TestLoginDropsForeignNext(t *testing.T) {
	f := newFlow(t, nil)
	f.seedUser("ada@example.com", "secret-password")
	code, _, h := f.post("/login", url.Values{
		"email": {"ada@example.com"}, "password": {"secret-password"},
		"next": {"https://evil.example/authorize"},
	}, nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/" {
		t.Fatalf("foreign next: %d %q", code, h.Get("Location"))
	}
}

func TestLockedAuthorizeResumesAfterUnlock(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	if code, _, _ := f.post("/lock", nil, nil); code != http.StatusSeeOther {
		t.Fatalf("lock: %d", code)
	}
	code, _, h := f.get(authorizePath("resume"), nil)
	if code != http.StatusSeeOther || !strings.HasPrefix(h.Get("Location"), "/unlock?next=") {
		t.Fatalf("locked authorize: %d %q", code, h.Get("Location"))
	}
	next := authorizePath("resume")
	code, _, h = f.post("/unlock", url.Values{"password": {"secret-password"}, "next": {next}}, nil)
	if code != http.StatusSeeOther || h.Get("Location") != next {
		t.Fatalf("unlock next: %d %q", code, h.Get("Location"))
	}
}

func TestLocaleCookieOverridesHeader(t *testing.T) {
	f := newFlow(t, nil)
	u, _ := url.Parse(f.server.URL)
	f.client.Jar.SetCookies(u, []*http.Cookie{{Name: "kura_locale", Value: "pt", Path: "/"}})
	_, body, _ := f.get("/login", map[string]string{"Accept-Language": "en"})
	mustContain(t, body, "Entrar")
	code, _, h := f.post("/locale", url.Values{"lang": {"en"}}, map[string]string{
		"Referer": f.server.URL + "/login",
	})
	if code != http.StatusSeeOther || h.Get("Location") != "/login" {
		t.Fatalf("locale: %d %q", code, h.Get("Location"))
	}
	_, body, _ = f.get("/login", map[string]string{"Accept-Language": "pt-BR"})
	mustContain(t, body, "Sign in")
}
