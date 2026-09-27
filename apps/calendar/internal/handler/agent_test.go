package handler

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/aquasp/kuracalendar/internal/config"
	"github.com/aquasp/kuracalendar/internal/store"
)

func TestAssistantRedirectOK(t *testing.T) {
	const base = "https://assistant.gettansu.com"
	ok := base + "/apps/calendar/callback"
	if !AssistantRedirectOK(base+"/", ok, "calendar") {
		t.Fatal("trailing slash on the origin should match")
	}
	if !AssistantRedirectOK(base, ok+"/", "calendar") {
		t.Fatal("trailing slash on the callback path should match")
	}
	for _, bad := range []string{
		"https://evil.example/apps/calendar/callback",
		base + "/apps/spend/callback",
		base + "/apps/calendar/callback?x=1",
		base + "/apps/calendar/callback#t",
		"http://assistant.gettansu.com/apps/calendar/callback",
	} {
		if AssistantRedirectOK(base, bad, "calendar") {
			t.Fatalf("accepted %s", bad)
		}
	}
	if AssistantRedirectOK("", ok, "calendar") {
		t.Fatal("empty origin")
	}
}

func agentPair(t *testing.T) (verifier, challenge, state string) {
	t.Helper()
	verifier = strings.Repeat("b", 43)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:]), strings.Repeat("c", 32)
}

func (f *flow) confirmAgent(redirectURI, state, challenge string) (int, string, http.Header) {
	return f.post("/agent/connect", url.Values{
		"redirect_uri":   {redirectURI},
		"state":          {state},
		"code_challenge": {challenge},
	}, nil)
}

func TestAgentConnectExchange(t *testing.T) {
	const origin = "http://assistant.test"
	redirect := origin + "/apps/calendar/callback"
	f := newFlow(t, func(c *config.Config) { c.KuraAssistantURL = origin })
	u := f.seedUser("you@example.com", "password1")
	if err := f.store.SetUserSub(u.ID, "sub-you"); err != nil {
		t.Fatal(err)
	}
	f.login(u.Email, "password1")
	verifier, challenge, state := agentPair(t)

	if code, body, _ := f.get("/agent/connect?redirect_uri="+url.QueryEscape("https://evil.example/apps/calendar/callback")+"&state="+state+"&code_challenge="+challenge+"&code_challenge_method=S256", nil); code != 400 || !strings.Contains(body, "not valid") && !strings.Contains(body, "não é válido") {
		t.Fatalf("bad redirect = %d %s", code, body)
	}

	code, body, _ := f.get("/agent/connect?redirect_uri="+url.QueryEscape(redirect)+"&state="+state+"&code_challenge="+challenge+"&code_challenge_method=S256", nil)
	if code != 200 || !strings.Contains(body, u.Email) || !strings.Contains(body, "locked") && !strings.Contains(body, "trancado") {
		t.Fatalf("confirm page = %d %s", code, body)
	}

	status, _, hdr := f.confirmAgent(redirect, state, challenge)
	if status != http.StatusSeeOther {
		t.Fatalf("confirm = %d", status)
	}
	loc, err := url.Parse(hdr.Get("Location"))
	if err != nil || loc.Query().Get("state") != state || loc.Query().Get("code") == "" {
		t.Fatalf("location %q", hdr.Get("Location"))
	}
	rawCode := loc.Query().Get("code")

	// A second confirm rotates the token but must leave a single named row.
	status2, _, hdr2 := f.confirmAgent(redirect, state, challenge)
	if status2 != http.StatusSeeOther {
		t.Fatalf("second confirm = %d", status2)
	}
	list, err := f.store.ListTokens(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != store.AssistantTokenName {
		t.Fatalf("tokens = %+v", list)
	}

	// The first code was for a token that the second mint deleted. Exchange
	// of the first code still returns that (now revoked) raw value; the
	// second code is the live one. PKCE is required either way.
	ex := func(code, ver string) (int, map[string]any) {
		payload, _ := json.Marshal(map[string]string{"code": code, "code_verifier": ver})
		req, _ := http.NewRequest(http.MethodPost, f.server.URL+"/agent/exchange", strings.NewReader(string(payload)))
		req.Header.Set("Content-Type", "application/json")
		resp, err := f.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		var out map[string]any
		_ = json.Unmarshal(b, &out)
		return resp.StatusCode, out
	}
	if st, _ := ex(rawCode, ""); st != http.StatusBadRequest {
		t.Fatalf("exchange without verifier = %d", st)
	}
	live := hdr2.Get("Location")
	liveURL, _ := url.Parse(live)
	st, body2 := ex(liveURL.Query().Get("code"), verifier)
	if st != 200 || body2["email"] != u.Email || body2["account_sub"] != "sub-you" || !strings.HasPrefix(body2["token"].(string), "kura_") {
		t.Fatalf("exchange = %d %#v", st, body2)
	}
	if st, _ := ex(liveURL.Query().Get("code"), verifier); st != http.StatusBadRequest {
		t.Fatalf("second exchange = %d", st)
	}

	tok := body2["token"].(string)
	del, _ := f.apiCall(http.MethodDelete, "/api/v1/token", tok, "")
	if del != http.StatusNoContent {
		t.Fatalf("revoke = %d", del)
	}
	again, _ := f.apiCall(http.MethodDelete, "/api/v1/token", tok, "")
	if again != http.StatusUnauthorized {
		t.Fatalf("token still works: %d", again)
	}
}

func TestAgentSSOUserCanConnect(t *testing.T) {
	const origin = "http://assistant.test"
	f := newFlow(t, func(c *config.Config) { c.KuraAssistantURL = origin })
	u, err := f.store.CreateUser("sso@example.com", "!kura!not-a-bcrypt-hash")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := f.store.CreateSession(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Plant the session cookie the way login would.
	f.client.Jar.SetCookies(mustURL(f.server.URL), []*http.Cookie{{
		Name: SessionCookie, Value: sess.ID, Path: "/",
	}})
	_, _, _ = f.get("/login", nil) // csrf
	verifier, challenge, state := agentPair(t)
	status, _, hdr := f.confirmAgent(origin+"/apps/calendar/callback", state, challenge)
	if status != http.StatusSeeOther || !strings.Contains(hdr.Get("Location"), "code=") {
		t.Fatalf("sso confirm = %d %s", status, hdr.Get("Location"))
	}
	_ = verifier
}

func TestAgentConnectRemembersReturn(t *testing.T) {
	const origin = "http://assistant.test"
	f := newFlow(t, func(c *config.Config) { c.KuraAssistantURL = origin })
	u := f.seedUser("back@example.com", "password1")
	verifier, challenge, state := agentPair(t)
	_ = verifier
	path := "/agent/connect?redirect_uri=" + url.QueryEscape(origin+"/apps/calendar/callback") +
		"&state=" + state + "&code_challenge=" + challenge + "&code_challenge_method=S256"
	code, _, hdr := f.get(path, nil)
	if code != http.StatusSeeOther || hdr.Get("Location") != "/login" {
		t.Fatalf("anon = %d %s", code, hdr.Get("Location"))
	}
	status, _, hdr := f.post("/login", url.Values{"email": {u.Email}, "password": {"password1"}}, nil)
	if status != http.StatusSeeOther || !strings.HasPrefix(hdr.Get("Location"), "/agent/connect?") {
		t.Fatalf("login return = %d %s", status, hdr.Get("Location"))
	}
}

func TestAgentConnectRateLimit(t *testing.T) {
	const origin = "http://assistant.test"
	f := newFlow(t, func(c *config.Config) { c.KuraAssistantURL = origin })
	u := f.seedUser("rate@example.com", "password1")
	f.login(u.Email, "password1")
	_, challenge, state := agentPair(t)
	path := "/agent/connect?redirect_uri=" + url.QueryEscape(origin+"/apps/calendar/callback") +
		"&state=" + state + "&code_challenge=" + challenge + "&code_challenge_method=S256"
	var code int
	for i := 0; i < 11; i++ {
		code, _, _ = f.get(path, nil)
	}
	if code != http.StatusTooManyRequests {
		t.Fatalf("11th connect = %d", code)
	}
}

func mustURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}
