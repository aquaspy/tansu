package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aquasp/kurachat/internal/chat"
	"github.com/aquasp/kurachat/internal/config"
	"github.com/aquasp/kurachat/internal/openrouter"
	"github.com/aquasp/kurachat/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// onePixel is a valid 1x1 PNG for upload tests.
var onePixel, _ = base64.StdEncoding.DecodeString(
	"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")

type stubLLM struct {
	events []map[string]any
	title  string
}

func (f *stubLLM) StreamChat(_ context.Context, _ []any, _ *int, _ string, _ string, _ *openrouter.SearchOptions, _ *openrouter.FileOptions, yield func(map[string]any) error) error {
	for _, e := range f.events {
		if err := yield(e); err != nil {
			return err
		}
	}
	return nil
}

func (f *stubLLM) Complete(_ context.Context, _ []any, _ *int, _ string) (map[string]any, error) {
	return map[string]any{"choices": []any{map[string]any{"message": map[string]any{
		"role": "assistant", "content": f.title}}}}, nil
}

type flow struct {
	t      *testing.T
	server *httptest.Server
	client *http.Client
	store  *store.Store
	stub   *stubLLM
	srv    *Server
}

func newFlow(t *testing.T, mutate func(*config.Config)) *flow {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	cfg := config.Config{
		DataDir: t.TempDir(), SignupEnabled: true,
		OpenRouterModel: "openai/gpt-6-luna", OpenRouterReasoningEffort: "high",
		SearchEngine: "exa", SearchMode: "auto", SearchMaxResults: 5,
		SearchDeepMode: "deep-lite", SearchDeepMaxResults: 10, SearchFeeIncluded: true,
		ChatWindowTokens: 150000, ChatKeepRecentTokens: 32000,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	stub := &stubLLM{events: []map[string]any{
		{"choices": []any{map[string]any{"delta": map[string]any{"content": "Stubbed."}}}},
		{"choices": []any{map[string]any{"delta": map[string]any{}, "finish_reason": "stop"}},
			"usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 2, "cost": 0.000001}},
	}, title: "Stub title"}
	svc := &chat.Service{
		Store: st, Hub: chat.NewHub(), DataDir: cfg.DataDir,
		Config: chat.CompleterConfig{
			Model: cfg.OpenRouterModel, Models: cfg.OpenRouterModels,
			Effort:       cfg.OpenRouterReasoningEffort,
			WindowTokens: cfg.ChatWindowTokens, KeepRecentTokens: cfg.ChatKeepRecentTokens,
			Search: chat.SearchConfig{
				Enabled: cfg.SearchEnabled, Engine: cfg.SearchEngine,
				Mode: cfg.SearchMode, MaxResults: cfg.SearchMaxResults,
				DeepMode: cfg.SearchDeepMode, DeepMaxResults: cfg.SearchDeepMaxResults,
				FeeIncluded: cfg.SearchFeeIncluded,
			},
		},
		APIKey:    "x",
		NewClient: func(string) (chat.LLMClient, error) { return stub, nil },
	}
	srv := NewServer(cfg, st, svc)
	srv.WebDir = t.TempDir()
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	f := &flow{t: t, server: ts, client: client, store: st, stub: stub, srv: srv}
	// Prime the CSRF cookie.
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

func (f *flow) post(path string, form url.Values, headers map[string]string) (int, string, http.Header) {
	if form == nil {
		form = url.Values{}
	}
	form.Set("csrf_token", f.csrf())
	req, _ := http.NewRequest(http.MethodPost, f.server.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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

func (f *flow) pollStatus(convID int64, want string) *store.Message {
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		rows, _ := f.store.Transcript(convID)
		for _, m := range rows {
			if m.Role == store.RoleAssistant && m.Status == want {
				return m
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	rows, _ := f.store.Transcript(convID)
	f.t.Fatalf("no %s assistant in %+v", want, rows)
	return nil
}

// pollCompleteCount waits until n assistants are complete (pollStatus
// matches the first one, which goes stale past turn one).
func (f *flow) pollCompleteCount(convID int64, n int) {
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		rows, _ := f.store.Transcript(convID)
		done := 0
		for _, m := range rows {
			if m.Role == store.RoleAssistant && m.Status == store.StatusComplete {
				done++
			}
		}
		if done >= n {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	rows, _ := f.store.Transcript(convID)
	f.t.Fatalf("only %d complete in %+v", n, rows)
}

func mustContain(t *testing.T, body, want string) {
	t.Helper()
	if !strings.Contains(body, want) {
		t.Fatalf("missing %q in body:\n%.1500s", want, body)
	}
}

func mustNotContain(t *testing.T, body, banned string) {
	t.Helper()
	if strings.Contains(body, banned) {
		t.Fatalf("found banned %q in body:\n%.1500s", banned, body)
	}
}

func TestAuthFlow(t *testing.T) {
	f := newFlow(t, nil)
	if code, _, _ := f.get("/signup", nil); code != 200 {
		t.Fatalf("signup page = %d", code)
	}
	code, _, h := f.post("/signup", url.Values{
		"email": {"you@x.com"}, "password": {"secret-ok"}, "password_confirmation": {"secret-ok"},
	}, nil)
	if code != 303 || h.Get("Location") != "/" {
		t.Fatalf("signup = %d %q", code, h.Get("Location"))
	}
	if code, body, _ := f.get("/", nil); code != 200 {
		t.Fatalf("index = %d", code)
	} else {
		mustContain(t, body, "TansuChat")
	}
	if code, _, h := f.post("/lock", nil, nil); code != 303 || h.Get("Location") != "/unlock" {
		t.Fatalf("lock = %d %q", code, h.Get("Location"))
	}
	if code, _, _ := f.post("/unlock", url.Values{"password": {"wrong-pass"}}, nil); code != 422 {
		t.Fatalf("bad unlock = %d", code)
	}
	if code, _, h := f.post("/unlock", url.Values{"password": {"secret-ok"}}, nil); code != 303 {
		t.Fatalf("unlock = %d %q", code, h.Get("Location"))
	}
	if code, _, h := f.methodCall(http.MethodDelete, "/logout", nil, nil); code != 303 || h.Get("Location") != "/login" {
		t.Fatalf("logout = %d %q", code, h.Get("Location"))
	}
	if code, _, h := f.get("/", nil); code != 303 || h.Get("Location") != "/login" {
		t.Fatalf("index after logout = %d %q", code, h.Get("Location"))
	}
}

func TestSessionCookiePersistent(t *testing.T) {
	f := newFlow(t, nil)
	f.seedUser("persist@x.com", "secret-ok")
	code, _, h := f.post("/login", url.Values{"email": {"persist@x.com"}, "password": {"secret-ok"}}, nil)
	if code != 303 {
		t.Fatalf("login = %d", code)
	}
	c := sessionCookie(t, h)
	if c.MaxAge < int((29 * 24 * time.Hour).Seconds()) {
		t.Fatalf("login cookie MaxAge = %d, want ~30 days", c.MaxAge)
	}
	if time.Until(c.Expires) < 29*24*time.Hour {
		t.Fatalf("login cookie Expires = %v, want ~30 days out", c.Expires)
	}
	// Authenticated requests renew the cookie (sliding window), so the
	// login survives browser restarts until 30 days idle.
	_, _, h = f.get("/", nil)
	c = sessionCookie(t, h)
	if c.MaxAge <= 0 || time.Until(c.Expires) < 29*24*time.Hour {
		t.Fatalf("renewed cookie = MaxAge %d Expires %v", c.MaxAge, c.Expires)
	}
}

func sessionCookie(t *testing.T, h http.Header) *http.Cookie {
	t.Helper()
	for _, c := range (&http.Response{Header: h}).Cookies() {
		if c.Name == SessionCookie {
			return c
		}
	}
	t.Fatalf("no %q cookie in %v", SessionCookie, h.Values("Set-Cookie"))
	return nil
}

func TestSignupClosed(t *testing.T) {
	f := newFlow(t, func(c *config.Config) { c.SignupEnabled = false })
	if code, _, h := f.get("/signup", nil); code != 303 || h.Get("Location") != "/login" {
		t.Fatalf("signup = %d %q", code, h.Get("Location"))
	}
}

func TestPTLocale(t *testing.T) {
	f := newFlow(t, nil)
	_, body, _ := f.get("/login", map[string]string{"Accept-Language": "pt-BR,pt;q=0.9"})
	mustContain(t, body, "Entrar")
	_, body, _ = f.get("/login", map[string]string{"Accept-Language": "pt-BR,pt;q=0.9"})
	mustContain(t, body, `lang="pt-BR"`)
}

func TestAutoLockCookie(t *testing.T) {
	f := newFlow(t, nil)
	f.seedUser("lock@x.com", "secret-ok")
	f.login("lock@x.com", "secret-ok")
	_, body, _ := f.get("/", nil)
	mustContain(t, body, "data-auto-lock-label>Turn on auto lock")
	// Backdate the unlock: without the cookie the session stays open.
	_, _ = f.store.DB().Exec(`UPDATE sessions SET unlocked_at = '2020-01-01 00:00:00'`)
	if code, _, _ := f.get("/", nil); code != 200 {
		t.Fatalf("index without cookie = %d", code)
	}
	u, _ := url.Parse(f.server.URL)
	f.client.Jar.SetCookies(u, []*http.Cookie{{Name: AutoLockCookie, Value: "1"}})
	if code, _, h := f.get("/", nil); code != 303 || h.Get("Location") != "/unlock" {
		t.Fatalf("stale autolock = %d %q", code, h.Get("Location"))
	}
}

func TestPasswordChange(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("pw@x.com", "secret-ok")
	f.login("pw@x.com", "secret-ok")
	mustContain(t, mustGet(t, f, "/password/edit"), "Current password")
	hx := map[string]string{"HX-Request": "true"}
	if code, _, _ := f.methodCall(http.MethodPatch, "/password", url.Values{
		"current_password": {"wrong-pass"}, "password": {"new-secret"}, "password_confirmation": {"new-secret"},
	}, hx); code != 422 {
		t.Fatalf("wrong current = %d", code)
	}
	if code, _, _ := f.methodCall(http.MethodPatch, "/password", url.Values{
		"current_password": {"secret-ok"}, "password": {"new-secret"}, "password_confirmation": {"mismatch!"},
	}, hx); code != 422 {
		t.Fatalf("mismatch = %d", code)
	}
	if code, _, _ := f.methodCall(http.MethodPatch, "/password", url.Values{
		"current_password": {"secret-ok"}, "password": {"short"}, "password_confirmation": {"short"},
	}, hx); code != 422 {
		t.Fatalf("short = %d", code)
	}
	code, _, h := f.methodCall(http.MethodPatch, "/password", url.Values{
		"current_password": {"secret-ok"}, "password": {"new-secret"}, "password_confirmation": {"new-secret"},
	}, hx)
	if code != 200 || h.Get("HX-Redirect") != "/" {
		t.Fatalf("change = %d redirect=%q", code, h.Get("HX-Redirect"))
	}
	mustContain(t, mustGet(t, f, "/"), "Password changed.")
	fresh, _ := f.store.FindUser(u.ID)
	if bcrypt.CompareHashAndPassword([]byte(fresh.PasswordDigest), []byte("new-secret")) != nil {
		t.Fatal("password not updated")
	}
	if code, _, _ := f.post("/login", url.Values{"email": {"pw@x.com"}, "password": {"secret-ok"}}, nil); code != 422 {
		t.Fatalf("old password login = %d", code)
	}
}

func mustGet(t *testing.T, f *flow, path string) string {
	t.Helper()
	code, body, _ := f.get(path, nil)
	if code != 200 {
		t.Fatalf("GET %s = %d", path, code)
	}
	return body
}

func TestPasswordRequiresSession(t *testing.T) {
	f := newFlow(t, nil)
	if code, _, h := f.get("/password/edit", nil); code != 303 || h.Get("Location") != "/login" {
		t.Fatalf("edit = %d %q", code, h.Get("Location"))
	}
}

func TestShareFlow(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("you@x.com", "secret-ok")
	conv, _ := f.store.CreateConversation(u.ID)
	_ = f.store.UpdateConversationTitle(u.ID, conv.ID, "Picnic")
	_, _ = f.store.CreateUserMessage(conv.ID, "Where?", false, false)
	a, _ := f.store.CreateAssistantMessage(conv.ID)
	_ = f.store.CompleteAssistant(a.ID, store.Completion{Content: "The park."})
	f.login("you@x.com", "secret-ok")

	hx := map[string]string{"HX-Request": "true"}
	if code, _, _ := f.post("/conversations/1/share", nil, hx); code != 200 {
		t.Fatalf("share create = %d", code)
	}
	conv, _ = f.store.FindConversation(u.ID, conv.ID)
	token := conv.ShareToken
	if token == "" {
		t.Fatal("no token")
	}
	code, body, _ := f.get("/s/"+token, nil)
	if code != 200 {
		t.Fatalf("shared = %d", code)
	}
	mustContain(t, body, "The park.")
	mustContain(t, body, "noindex")
	mustNotContain(t, body, `class="composer"`)
	mustNotContain(t, body, `name="content"`)
	if code, _, _ := f.methodCall(http.MethodPatch, "/conversations/1/share", nil, hx); code != 200 {
		t.Fatalf("rotate = %d", code)
	}
	conv, _ = f.store.FindConversation(u.ID, conv.ID)
	if conv.ShareToken == token {
		t.Fatal("token not rotated")
	}
	if code, _, _ := f.get("/s/"+token, nil); code != 404 {
		t.Fatalf("old token = %d", code)
	}
	if code, _, _ := f.methodCall(http.MethodDelete, "/conversations/1/share", nil, hx); code != 200 {
		t.Fatalf("revoke = %d", code)
	}
	conv, _ = f.store.FindConversation(u.ID, conv.ID)
	if conv.ShareToken != "" {
		t.Fatal("still shared")
	}
}

func TestChatCRUD(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("you@x.com", "secret-ok")
	f.login("you@x.com", "secret-ok")
	code, _, h := f.post("/conversations/", nil, nil)
	if code != 303 {
		t.Fatalf("create = %d", code)
	}
	loc := h.Get("Location") // /conversations/1
	code, _, h = f.post("/conversations/", nil, nil)
	if code != 303 || h.Get("Location") != loc {
		t.Fatalf("draft reuse = %d %q", code, h.Get("Location"))
	}
	if n, _ := f.store.CountConversations(u.ID); n != 1 {
		t.Fatalf("count = %d", n)
	}
	hx := map[string]string{"HX-Request": "true"}
	code, body, _ := f.methodCall(http.MethodPatch, loc, url.Values{"conversation[title]": {"Taxes"}}, hx)
	if code != 200 {
		t.Fatalf("rename = %d", code)
	}
	mustContain(t, body, "Taxes")
	mustContain(t, mustGet(t, f, "/conversations/"), "Taxes")
	if code, _, _ := f.methodCall(http.MethodDelete, loc, nil, nil); code != 200 {
		t.Fatalf("delete = %d", code)
	}
	if _, err := f.store.FindConversation(u.ID, 1); err != store.ErrNotFound {
		t.Fatalf("err = %v", err)
	}
}

func TestDraftDiscardedOnLeave(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("you@x.com", "secret-ok")
	f.login("you@x.com", "secret-ok")
	_, _, h := f.post("/conversations/", nil, nil)
	if !strings.HasSuffix(h.Get("Location"), "/conversations/1") {
		t.Fatalf("loc = %q", h.Get("Location"))
	}
	mustGet(t, f, "/conversations/")
	if _, err := f.store.FindConversation(u.ID, 1); err != store.ErrNotFound {
		t.Fatalf("draft survived: %v", err)
	}
}

func TestStranger404(t *testing.T) {
	f := newFlow(t, nil)
	other := f.seedUser("them@x.com", "secret-ok")
	secret, _ := f.store.CreateConversation(other.ID)
	_ = f.store.UpdateConversationTitle(other.ID, secret.ID, "Secret")
	f.seedUser("you@x.com", "secret-ok")
	f.login("you@x.com", "secret-ok")
	if code, _, _ := f.get("/conversations/1", nil); code != 404 {
		t.Fatalf("stranger = %d", code)
	}
	hx := map[string]string{"HX-Request": "true"}
	if code, _, _ := f.methodCall(http.MethodPatch, "/conversations/1/settings",
		url.Values{"model": {"x"}}, hx); code != 404 {
		t.Fatalf("stranger settings = %d", code)
	}
}

func enableSearch(c *config.Config) { c.SearchEnabled = true }

func TestWebSearchFlow(t *testing.T) {
	f := newFlow(t, enableSearch)
	u := f.seedUser("you@x.com", "secret-ok")
	conv, _ := f.store.CreateConversation(u.ID)
	f.login("you@x.com", "secret-ok")
	hx := map[string]string{"HX-Request": "true"}
	form := url.Values{"content": {"News?"}, "web": {"1"}}
	if code, _, _ := f.post("/conversations/1/messages", form, hx); code != 200 {
		t.Fatalf("post = %d", code)
	}
	conv, _ = f.store.FindConversation(u.ID, conv.ID)
	if !conv.WebSearch {
		t.Fatal("sticky web should be on")
	}
	done := f.pollStatus(conv.ID, store.StatusComplete)
	if !strings.Contains(done.TokenUsage, `"search_engine":"exa"`) ||
		!strings.Contains(done.TokenUsage, `"search_cost_usd":0.007`) {
		t.Fatalf("usage = %q", done.TokenUsage)
	}
	if strings.Contains(done.TokenUsage, "search_cost_separate") {
		t.Fatalf("included fee must not be separate: %q", done.TokenUsage)
	}
	body := mustGet(t, f, "/conversations/1")
	mustContain(t, body, "search-picker")
	mustContain(t, body, `name="web"`)
	mustContain(t, body, `name="deep"`)
	mustContain(t, body, "msg-meta")
	mustContain(t, body, "openai/gpt-6-luna")
}

func TestDeepSearchFlow(t *testing.T) {
	f := newFlow(t, enableSearch)
	u := f.seedUser("you@x.com", "secret-ok")
	conv, _ := f.store.CreateConversation(u.ID)
	f.login("you@x.com", "secret-ok")
	hx := map[string]string{"HX-Request": "true"}
	form := url.Values{"content": {"Deep dive?"}, "web": {"1"}, "deep": {"1"}}
	if code, _, _ := f.post("/conversations/1/messages", form, hx); code != 200 {
		t.Fatalf("post = %d", code)
	}
	done := f.pollStatus(conv.ID, store.StatusComplete)
	if !strings.Contains(done.TokenUsage, `"search_deep":true`) ||
		!strings.Contains(done.TokenUsage, `"search_cost_usd":0.012`) {
		t.Fatalf("usage = %q", done.TokenUsage)
	}
	conv, _ = f.store.FindConversation(u.ID, conv.ID)
	if !conv.DeepSearch {
		t.Fatal("sticky deep should be on")
	}
	body := mustGet(t, f, "/conversations/1")
	mustContain(t, body, ">deep<")
}

func TestWebParamIgnoredWhenDisabled(t *testing.T) {
	f := newFlow(t, nil) // SearchEnabled false
	u := f.seedUser("you@x.com", "secret-ok")
	conv, _ := f.store.CreateConversation(u.ID)
	f.login("you@x.com", "secret-ok")
	hx := map[string]string{"HX-Request": "true"}
	form := url.Values{"content": {"News?"}, "web": {"1"}}
	if code, _, _ := f.post("/conversations/1/messages", form, hx); code != 200 {
		t.Fatalf("post = %d", code)
	}
	rows, _ := f.store.Transcript(conv.ID)
	for _, m := range rows {
		if m.Role == store.RoleUser && m.Web {
			t.Fatal("web flag should stay off")
		}
	}
	conv, _ = f.store.FindConversation(u.ID, conv.ID)
	if conv.WebSearch {
		t.Fatal("sticky web should stay off")
	}
	body := mustGet(t, f, "/conversations/1")
	mustNotContain(t, body, "search-picker")
	mustNotContain(t, body, "model-picker")
	mustContain(t, body, "effort-picker")
}

func TestModelPicker(t *testing.T) {
	f := newFlow(t, func(c *config.Config) {
		c.OpenRouterModels = []string{"openai/gpt-6-luna", "x-ai/grok-4.7"}
	})
	u := f.seedUser("you@x.com", "secret-ok")
	conv, _ := f.store.CreateConversation(u.ID)
	f.login("you@x.com", "secret-ok")
	body := mustGet(t, f, "/conversations/1")
	mustContain(t, body, "chat-settings")
	mustContain(t, body, "model-picker")
	mustContain(t, body, "model-menu")
	mustContain(t, body, `name="model"`)
	mustContain(t, body, "effort-picker")
	mustContain(t, body, `name="effort"`)
	mustContain(t, body, `type="radio"`)
	mustContain(t, body, "x-ai/grok-4.7")
	hx := map[string]string{"HX-Request": "true"}
	// Effort flips persist; unknown values reset to the default.
	if code, _, _ := f.methodCall(http.MethodPatch, "/conversations/1/settings",
		url.Values{"effort": {"low"}}, hx); code != 200 {
		t.Fatalf("settings = %d", code)
	}
	conv, _ = f.store.FindConversation(u.ID, conv.ID)
	if conv.Effort != "low" {
		t.Fatalf("effort = %q", conv.Effort)
	}
	if code, _, _ := f.methodCall(http.MethodPatch, "/conversations/1/settings",
		url.Values{"effort": {"bogus"}}, hx); code != 200 {
		t.Fatalf("settings = %d", code)
	}
	conv, _ = f.store.FindConversation(u.ID, conv.ID)
	if conv.Effort != "" {
		t.Fatalf("effort = %q", conv.Effort)
	}
	if code, _, _ := f.methodCall(http.MethodPatch, "/conversations/1/settings",
		url.Values{"model": {"x-ai/grok-4.7"}}, hx); code != 200 {
		t.Fatalf("settings = %d", code)
	}
	conv, _ = f.store.FindConversation(u.ID, conv.ID)
	if conv.Model != "x-ai/grok-4.7" {
		t.Fatalf("model = %q", conv.Model)
	}
	// Unknown slugs reset to the server default.
	if code, _, _ := f.methodCall(http.MethodPatch, "/conversations/1/settings",
		url.Values{"model": {"ghost/model"}}, hx); code != 200 {
		t.Fatalf("settings = %d", code)
	}
	conv, _ = f.store.FindConversation(u.ID, conv.ID)
	if conv.Model != "" {
		t.Fatalf("model = %q", conv.Model)
	}
	// Partial update: web_search alone keeps the model.
	_ = f.store.UpdateConversationSettings(u.ID, conv.ID, store.ConversationSettings{Model: "x-ai/grok-4.7"})
	if code, _, _ := f.methodCall(http.MethodPatch, "/conversations/1/settings",
		url.Values{"web_search": {"1"}}, hx); code != 200 {
		t.Fatalf("settings = %d", code)
	}
	conv, _ = f.store.FindConversation(u.ID, conv.ID)
	if conv.Model != "x-ai/grok-4.7" || conv.WebSearch {
		t.Fatalf("conv = %+v", conv) // search disabled: forced off
	}
	// Sending pins both sticky values at once.
	form := url.Values{"content": {"Hi"}, "model": {"x-ai/grok-4.7"}}
	if code, _, _ := f.post("/conversations/1/messages", form, hx); code != 200 {
		t.Fatalf("post = %d", code)
	}
	conv, _ = f.store.FindConversation(u.ID, conv.ID)
	if conv.Model != "x-ai/grok-4.7" {
		t.Fatalf("model = %q", conv.Model)
	}
	// Plain sends (model param absent) keep the sticky model.
	f.pollCompleteCount(conv.ID, 1)
	if code, _, _ := f.post("/conversations/1/messages", url.Values{"content": {"Hi again"}}, hx); code != 200 {
		t.Fatalf("post = %d", code)
	}
	conv, _ = f.store.FindConversation(u.ID, conv.ID)
	if conv.Model != "x-ai/grok-4.7" {
		t.Fatalf("model = %q", conv.Model)
	}
	// The first turn ran on the sticky model (stub reports no model, so
	// the completer stamps the resolved one).
	rows, _ := f.store.Transcript(conv.ID)
	if !strings.Contains(rows[1].TokenUsage, `"model":"x-ai/grok-4.7"`) {
		t.Fatalf("usage = %q", rows[1].TokenUsage)
	}
	// Multipart sends (images) keep it too.
	f.pollCompleteCount(conv.ID, 2)
	if code, _ := testImageUpload(t, f, "1", 1); code != 200 {
		t.Fatalf("upload = %d", code)
	}
	conv, _ = f.store.FindConversation(u.ID, conv.ID)
	if conv.Model != "x-ai/grok-4.7" {
		t.Fatalf("model = %q", conv.Model)
	}
	rows, _ = f.store.Transcript(conv.ID)
	if users := countRole(rows, store.RoleUser); users != 3 {
		t.Fatalf("users = %d", users)
	}
	// Non-hx settings submit redirects back to the chat.
	if code, _, h := f.methodCall(http.MethodPatch, "/conversations/1/settings",
		url.Values{"model": {"openai/gpt-6-luna"}}, nil); code != 303 ||
		!strings.HasSuffix(h.Get("Location"), "/conversations/1") {
		t.Fatalf("settings = %d %q", code, h.Get("Location"))
	}
}

func countRole(rows []*store.Message, role string) int {
	n := 0
	for _, m := range rows {
		if m.Role == role {
			n++
		}
	}
	return n
}

func TestInflightRejected(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("you@x.com", "secret-ok")
	conv, _ := f.store.CreateConversation(u.ID)
	_, _ = f.store.CreateUserMessage(conv.ID, "Hi", false, false)
	a, _ := f.store.CreateAssistantMessage(conv.ID)
	_ = f.store.SetAssistantStreaming(a.ID)
	f.login("you@x.com", "secret-ok")
	hx := map[string]string{"HX-Request": "true"}
	code, _, h := f.post("/conversations/1/messages", url.Values{"content": {"Again"}}, hx)
	if code != 200 || !strings.HasSuffix(h.Get("HX-Redirect"), "/conversations/1") {
		t.Fatalf("post = %d redirect=%q", code, h.Get("HX-Redirect"))
	}
	rows, _ := f.store.Transcript(conv.ID)
	users := 0
	for _, m := range rows {
		if m.Role == store.RoleUser {
			users++
		}
	}
	if users != 1 {
		t.Fatalf("users = %d", users)
	}
}

func testImageUpload(t *testing.T, f *flow, convID string, n int) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("content", "Compare")
	_ = mw.WriteField("csrf_token", f.csrf())
	for i := 0; i < n; i++ {
		w, err := mw.CreateFormFile("files[]", "dot.png")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(onePixel)
	}
	_ = mw.Close()
	req, _ := http.NewRequest(http.MethodPost, f.server.URL+"/conversations/"+convID+"/messages", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("HX-Request", "true")
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestImageUploads(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("you@x.com", "secret-ok")
	conv, _ := f.store.CreateConversation(u.ID)
	f.login("you@x.com", "secret-ok")
	if code, body := testImageUpload(t, f, "1", 4); code != 200 {
		t.Fatalf("upload4 = %d %.300s", code, body)
	}
	rows, _ := f.store.Transcript(conv.ID)
	if len(rows) == 0 || len(rows[0].Images) != 4 {
		t.Fatalf("rows = %+v", rows)
	}
	// Fifth image rejected; nothing stored.
	if code, _ := testImageUpload(t, f, "1", 5); code != 200 {
		t.Fatalf("upload5 = %d", code)
	}
	rows, _ = f.store.Transcript(conv.ID)
	users := 0
	for _, m := range rows {
		if m.Role == store.RoleUser {
			users++
		}
	}
	if users != 1 {
		t.Fatalf("users = %d", users)
	}
	// Served back with the stored type.
	imgID := rows[0].Images[0].ID
	code, body, h := f.get("/images/"+itoa64(imgID)+"?v=thumb", nil)
	if code != 200 || h.Get("Content-Type") != "image/jpeg" {
		t.Fatalf("thumb = %d %q", code, h.Get("Content-Type"))
	}
	if len(body) == 0 {
		t.Fatal("empty thumb")
	}
}

func testDocUpload(t *testing.T, f *flow, convID string) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("content", "Summarize")
	_ = mw.WriteField("csrf_token", f.csrf())
	w, err := mw.CreateFormFile("files[]", "r.pdf")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\ntrailer\n<<>>\n"))
	_ = mw.Close()
	req, _ := http.NewRequest(http.MethodPost, f.server.URL+"/conversations/"+convID+"/messages", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("HX-Request", "true")
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestDocumentUpload(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("you@x.com", "secret-ok")
	conv, _ := f.store.CreateConversation(u.ID)
	f.login("you@x.com", "secret-ok")
	code, body := testDocUpload(t, f, "1")
	if code != 200 {
		t.Fatalf("upload = %d %.300s", code, body)
	}
	mustContain(t, body, "msg-doc")
	mustContain(t, body, "r.pdf")
	rows, _ := f.store.Transcript(conv.ID)
	if len(rows) == 0 || len(rows[0].Documents) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	// Served back with the stored type.
	docID := rows[0].Documents[0].ID
	code, body, h := f.get("/documents/"+itoa64(docID), nil)
	if code != 200 || h.Get("Content-Type") != "application/pdf" {
		t.Fatalf("doc = %d %q", code, h.Get("Content-Type"))
	}
	if len(body) == 0 {
		t.Fatal("empty doc")
	}
}

func TestSearchFilters(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("you@x.com", "secret-ok")
	g, _ := f.store.CreateConversation(u.ID)
	_ = f.store.UpdateConversationTitle(u.ID, g.ID, "Garden")
	tx, _ := f.store.CreateConversation(u.ID)
	_ = f.store.UpdateConversationTitle(u.ID, tx.ID, "Taxes")
	f.login("you@x.com", "secret-ok")
	_, body, _ := f.get("/conversations/?q=Tax", nil)
	mustContain(t, body, "Taxes")
	mustNotContain(t, body, "Garden")
	code, partial, _ := f.get("/conversations/?q=Tax", map[string]string{"HX-Request": "true"})
	if code != 200 {
		t.Fatalf("partial = %d", code)
	}
	mustContain(t, partial, "Taxes")
	mustContain(t, partial, `id="conversation-list"`)
	mustNotContain(t, partial, "col-editor")
}

// The rename input subscribes to the auto-title SSE event, but its
// enclosing form targets #conversation-list for renames — and SSE swaps
// inherit hx-target. Without its own target the first auto-title replaced
// the whole sidebar with the input.
func TestTitleFieldSelfTarget(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("you@x.com", "secret-ok")
	_, _ = f.store.CreateConversation(u.ID)
	f.login("you@x.com", "secret-ok")
	body := mustGet(t, f, "/conversations/1")
	mustContain(t, body, `sse-swap="title-field-1"`)
	mustContain(t, body, `hx-target="this"`)
}

func TestDeleteAllPurges(t *testing.T) {
	f := newFlow(t, nil)
	other := f.seedUser("them@x.com", "secret-ok")
	keep, _ := f.store.CreateConversation(other.ID)
	_ = f.store.UpdateConversationTitle(other.ID, keep.ID, "Theirs")
	u := f.seedUser("you@x.com", "secret-ok")
	mine, _ := f.store.CreateConversation(u.ID)
	msg, _ := f.store.CreateUserMessage(mine.ID, "secret", false, false)
	img, _ := f.store.CreateImage(msg.ID, "dot.png", "image/png", 1, "abc123")
	_ = img
	f.login("you@x.com", "secret-ok")
	if code, _, _ := f.methodCall(http.MethodDelete, "/conversations/", nil, nil); code != 200 {
		t.Fatalf("delete-all = %d", code)
	}
	if _, err := f.store.FindConversation(u.ID, mine.ID); err != store.ErrNotFound {
		t.Fatalf("mine survived: %v", err)
	}
	if _, err := f.store.FindConversation(other.ID, keep.ID); err != nil {
		t.Fatalf("theirs gone: %v", err)
	}
	mustContain(t, mustGet(t, f, "/conversations/"), "All chats deleted.")
}

func TestCostPill(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("you@x.com", "secret-ok")
	conv, _ := f.store.CreateConversation(u.ID)
	a, _ := f.store.CreateAssistantMessage(conv.ID)
	_ = f.store.CompleteAssistant(a.ID, store.Completion{Content: "Hello",
		TokenUsage: `{"input_tokens":80000,"cached_tokens":0,"output_tokens":20000,"reasoning_tokens":0,"cost_usd":0.125}`})
	f.login("you@x.com", "secret-ok")
	body := mustGet(t, f, "/conversations/1")
	mustContain(t, body, "chat-cost")
	mustContain(t, body, "$0.125")
	mustNotContain(t, body, "est. $0.12")
}

func TestCompletionEndToEnd(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("you@x.com", "secret-ok")
	conv, _ := f.store.CreateConversation(u.ID)
	f.login("you@x.com", "secret-ok")
	hx := map[string]string{"HX-Request": "true"}
	if code, body, _ := f.post("/conversations/1/messages", url.Values{"content": {"Hello"}}, hx); code != 200 {
		t.Fatalf("post = %d %.300s", code, body)
	}
	done := f.pollStatus(conv.ID, store.StatusComplete)
	if done.Content != "Stubbed." {
		t.Fatalf("content = %q", done.Content)
	}
	conv, _ = f.store.FindConversation(u.ID, conv.ID)
	if conv.Title != "Stub title" {
		t.Fatalf("title = %q", conv.Title)
	}
}
