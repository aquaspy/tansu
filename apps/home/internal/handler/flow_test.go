package handler

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aquasp/kurahome/internal/config"
	"github.com/aquasp/kurahome/internal/store"
	"golang.org/x/crypto/bcrypt"
)

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
	srv := NewServer(cfg, st)
	srv.WebDir = t.TempDir()
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	f := &flow{t: t, server: ts, client: client, store: st, srv: srv}
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

func (f *flow) patch(path string, form url.Values, headers map[string]string) (int, string, http.Header) {
	return f.methodCall(http.MethodPatch, path, form, headers)
}

func (f *flow) delete(path string, headers map[string]string) (int, string, http.Header) {
	return f.methodCall(http.MethodDelete, path, nil, headers)
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
	if _, err := f.store.CreateProfile(u.ID, "Personal", 0); err != nil {
		f.t.Fatal(err)
	}
	return u
}

func (f *flow) login(email, password string) {
	code, _, h := f.post("/login", url.Values{"email": {email}, "password": {password}}, nil)
	if code != http.StatusSeeOther {
		f.t.Fatalf("login status = %d", code)
	}
	if loc := h.Get("Location"); loc != "/" {
		f.t.Fatalf("login redirect = %q", loc)
	}
}

func mustContain(t *testing.T, body, want string) {
	t.Helper()
	if !strings.Contains(body, want) {
		t.Fatalf("body missing %q", want)
	}
}

func mustNotContain(t *testing.T, body, want string) {
	t.Helper()
	if strings.Contains(body, want) {
		t.Fatalf("body unexpectedly contains %q", want)
	}
}

func TestSignupCanBeTurnedOff(t *testing.T) {
	f := newFlow(t, func(c *config.Config) { c.SignupEnabled = false })

	code, _, h := f.get("/signup", nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/login" {
		t.Fatalf("GET /signup = %d -> %q", code, h.Get("Location"))
	}
	code, body, _ := f.get("/login", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /login = %d", code)
	}
	mustNotContain(t, body, "Create one")

	code, _, _ = f.post("/signup", url.Values{
		"email": {"intruder@example.com"}, "password": {"secret-password"},
		"password_confirmation": {"secret-password"},
	}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("POST /signup = %d", code)
	}
	if _, err := f.store.FindUserByEmail("intruder@example.com"); err == nil {
		t.Fatal("intruder user was created")
	}
}

func TestSignupCreatesUserWithHomeProfile(t *testing.T) {
	f := newFlow(t, nil)
	code, _, _ := f.post("/signup", url.Values{
		"email": {"lin@example.com"}, "password": {"secret-password"},
		"password_confirmation": {"secret-password"},
	}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("POST /signup = %d", code)
	}
	user, err := f.store.FindUserByEmail("lin@example.com")
	if err != nil {
		t.Fatal("user not created")
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordDigest), []byte("secret-password")) != nil {
		t.Fatal("password not stored")
	}
	if n := f.store.CountProfiles(user.ID); n != 1 {
		t.Fatalf("profiles = %d, want 1", n)
	}
}

func TestSessionCookiePersistent(t *testing.T) {
	f := newFlow(t, nil)
	f.seedUser("persist@x.com", "secret-ok")
	code, _, h := f.post("/login", url.Values{"email": {"persist@x.com"}, "password": {"secret-ok"}}, nil)
	if code != http.StatusSeeOther {
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

func TestLoginOpensHome(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")

	code, body, _ := f.get("/", nil)
	if code != http.StatusOK {
		t.Fatalf("GET / = %d", code)
	}
	mustContain(t, body, "TansuHome")
	mustContain(t, body, "Personal")
	mustContain(t, body, "data-edit-target=\"label\"")
}

func TestStrangersCannotReadHome(t *testing.T) {
	f := newFlow(t, nil)
	code, _, h := f.get("/", nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/login" {
		t.Fatalf("GET / = %d -> %q", code, h.Get("Location"))
	}
	code, _, h = f.get("/stack", nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/login" {
		t.Fatalf("GET /stack = %d -> %q", code, h.Get("Location"))
	}
}

func TestAddProfileAndSite(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")

	code, _, h := f.post("/profiles", url.Values{"profile[name]": {"Work"}}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("POST /profiles = %d", code)
	}
	profiles, _ := f.store.ListProfiles(u.ID)
	if len(profiles) != 2 {
		t.Fatalf("profiles = %d, want 2", len(profiles))
	}
	var work *store.Profile
	for _, p := range profiles {
		if p.Name == "Work" {
			work = p
		}
	}
	if work == nil {
		t.Fatal("Work profile missing")
	}
	if want := "/?profile_id=" + itoa64(work.ID); h.Get("Location") != want {
		t.Fatalf("redirect = %q, want %q", h.Get("Location"), want)
	}

	code, _, _ = f.post("/sites", url.Values{
		"site[profile_id]": {itoa64(work.ID)},
		"site[title]":      {"Faculty"},
		"site[url]":        {"faculdade.edu.br"},
		"site[hint]":       {"Portal"},
	}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("POST /sites = %d", code)
	}
	sites, _ := f.store.ListSites(work.ID)
	if len(sites) != 1 {
		t.Fatalf("sites = %d, want 1", len(sites))
	}
	if sites[0].URL != "https://faculdade.edu.br" {
		t.Fatalf("url = %q", sites[0].URL)
	}
	code, body, _ := f.get("/?profile_id="+itoa64(work.ID), nil)
	if code != http.StatusOK {
		t.Fatalf("GET / = %d", code)
	}
	mustContain(t, body, "Faculty")
	mustContain(t, body, "Portal")
	mustContain(t, body, `<a class="tile" href="https://faculdade.edu.br"`)
	mustContain(t, body, "faculdade.edu.br")
	mustContain(t, body, "tile-edit is-danger")
}

func TestSitesCanBeReordered(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	profiles, _ := f.store.ListProfiles(u.ID)
	first, _ := f.store.CreateSite(&store.Site{ProfileID: profiles[0].ID, Title: "A", URL: "https://a.example", Position: 0})
	second, _ := f.store.CreateSite(&store.Site{ProfileID: profiles[0].ID, Title: "B", URL: "https://b.example", Position: 1})

	code, _, _ := f.patch("/sites/reorder", url.Values{"ids[]": {itoa64(second.ID), itoa64(first.ID)}}, nil)
	if code != http.StatusOK {
		t.Fatalf("PATCH /sites/reorder = %d", code)
	}
	sites, _ := f.store.ListSites(profiles[0].ID)
	if len(sites) != 2 || sites[0].ID != second.ID || sites[1].ID != first.ID {
		t.Fatalf("order = %v", sites)
	}
}

func TestAnotherUserCannotTouchSite(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	profiles, _ := f.store.ListProfiles(u.ID)
	site, _ := f.store.CreateSite(&store.Site{ProfileID: profiles[0].ID, Title: "Mine", URL: "https://example.com"})
	f.post("/logout", nil, nil)

	other := f.seedUser("other@example.com", "secret-password")
	f.login(other.Email, "secret-password")

	code, _, _ := f.patch("/sites/"+itoa64(site.ID), url.Values{"site[title]": {"Stolen"}}, nil)
	if code != http.StatusNotFound {
		t.Fatalf("PATCH foreign site = %d, want 404", code)
	}
	fresh, _ := f.store.FindOwnedSite(u.ID, site.ID)
	if fresh.Title != "Mine" {
		t.Fatalf("title = %q", fresh.Title)
	}
	code, _, _ = f.delete("/sites/"+itoa64(site.ID), nil)
	if code != http.StatusNotFound {
		t.Fatalf("DELETE foreign site = %d, want 404", code)
	}
	if _, err := f.store.FindOwnedSite(u.ID, site.ID); err != nil {
		t.Fatal("site was deleted")
	}
}

func TestLastProfileCannotBeDeleted(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	profiles, _ := f.store.ListProfiles(u.ID)

	code, _, h := f.delete("/profiles/"+itoa64(profiles[0].ID), nil)
	if code != http.StatusSeeOther {
		t.Fatalf("DELETE /profiles = %d", code)
	}
	if want := "/?profile_id=" + itoa64(profiles[0].ID); h.Get("Location") != want {
		t.Fatalf("redirect = %q, want %q", h.Get("Location"), want)
	}
	code, body, _ := f.get(h.Get("Location"), nil)
	if code != http.StatusOK {
		t.Fatalf("GET / = %d", code)
	}
	mustContain(t, body, "Keep at least one profile.")
	if _, err := f.store.FindProfile(u.ID, profiles[0].ID); err != nil {
		t.Fatal("last profile was deleted")
	}
}

func TestLockHidesHomeUntilUnlocked(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	profiles, _ := f.store.ListProfiles(u.ID)
	_, _ = f.store.CreateSite(&store.Site{ProfileID: profiles[0].ID, Title: "Hidden after lock", URL: "https://example.com"})

	code, _, h := f.post("/lock", nil, nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/unlock" {
		t.Fatalf("POST /lock = %d -> %q", code, h.Get("Location"))
	}
	code, body, _ := f.get("/unlock", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /unlock = %d", code)
	}
	mustNotContain(t, body, "Hidden after lock")

	code, _, h = f.get("/", nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/unlock" {
		t.Fatalf("GET / locked = %d -> %q", code, h.Get("Location"))
	}
	code, _, _ = f.post("/unlock", url.Values{"password": {"wrong-password"}}, nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("POST /unlock wrong = %d", code)
	}
	code, _, _ = f.post("/unlock", url.Values{"password": {"secret-password"}}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("POST /unlock = %d", code)
	}
	code, body, _ = f.get("/", nil)
	if code != http.StatusOK {
		t.Fatalf("GET / = %d", code)
	}
	mustContain(t, body, "Hidden after lock")
}

func TestUILanguageFollowsAcceptLanguage(t *testing.T) {
	f := newFlow(t, nil)
	code, body, _ := f.get("/login", map[string]string{"Accept-Language": "pt-BR,pt;q=0.9"})
	if code != http.StatusOK {
		t.Fatalf("GET /login = %d", code)
	}
	mustContain(t, body, "Entrar")
	mustContain(t, body, `lang="pt-BR"`)

	code, body, _ = f.get("/login", map[string]string{"Accept-Language": "en-US,en;q=0.8"})
	if code != http.StatusOK {
		t.Fatalf("GET /login = %d", code)
	}
	mustContain(t, body, "Sign in")
	mustContain(t, body, `lang="en"`)
}

func TestLoggedInUserCanChangePassword(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")

	code, body, _ := f.get("/", nil)
	if code != http.StatusOK {
		t.Fatalf("GET / = %d", code)
	}
	mustContain(t, body, "Change password")

	code, _, h := f.patch("/password", url.Values{
		"current_password": {"secret-password"}, "password": {"new-secret"},
		"password_confirmation": {"new-secret"},
	}, nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/" {
		t.Fatalf("PATCH /password = %d -> %q", code, h.Get("Location"))
	}
	code, body, _ = f.get("/", nil)
	if code != http.StatusOK {
		t.Fatalf("GET / = %d", code)
	}
	mustContain(t, body, "Password changed.")

	f.post("/logout", nil, nil)
	code, _, _ = f.post("/login", url.Values{"email": {u.Email}, "password": {"secret-password"}}, nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("old password login = %d, want 422", code)
	}
	f.login(u.Email, "new-secret")
}

func TestPortugueseHomeLabels(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	code, body, _ := f.get("/", map[string]string{"Accept-Language": "pt-BR,pt;q=0.9"})
	if code != http.StatusOK {
		t.Fatalf("GET / = %d", code)
	}
	mustContain(t, body, "Adicionar site")
	mustContain(t, body, "Editar")
	mustNotContain(t, body, "Add a site")
}

func TestStackLivesOnOwnPagePerProfile(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")

	code, body, _ := f.get("/stack", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /stack = %d", code)
	}
	mustContain(t, body, "My stack")
	mustContain(t, body, "No stack yet")
	mustContain(t, body, `class="view-tab is-on">Stack<`)

	profiles, _ := f.store.ListProfiles(u.ID)
	code, _, h := f.post("/stack_items", url.Values{
		"stack_item[profile_id]": {itoa64(profiles[0].ID)},
		"stack_item[category]":   {"Music"},
		"stack_item[choice]":     {"Tidal"},
		"stack_item[origin]":     {"USA / Norway"},
		"stack_item[note]":       {"Good pick"},
		"stack_item[url]":        {"tidal.com"},
	}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("POST /stack_items = %d", code)
	}
	if want := "/stack?profile_id=" + itoa64(profiles[0].ID); h.Get("Location") != want {
		t.Fatalf("redirect = %q, want %q", h.Get("Location"), want)
	}
	items, _ := f.store.ListStackItems(profiles[0].ID)
	if len(items) != 1 || items[0].URL != "https://tidal.com" {
		t.Fatalf("items = %+v", items)
	}
	code, body, _ = f.get("/stack?profile_id="+itoa64(profiles[0].ID), nil)
	if code != http.StatusOK {
		t.Fatalf("GET /stack = %d", code)
	}
	mustContain(t, body, "Tidal")
	mustContain(t, body, "USA / Norway")
	mustContain(t, body, `<a href="https://tidal.com"`)
	mustContain(t, body, "tidal.com")
	mustContain(t, body, "share-btn")
	mustContain(t, body, "row-edit is-danger")
	mustContain(t, body, `data-controller="share"`)
	mustContain(t, body, "/stack_items/"+itoa64(items[0].ID)+"/icon")
}

func TestAnotherUserCannotTouchStackItem(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	profiles, _ := f.store.ListProfiles(u.ID)
	item, _ := f.store.CreateStackItem(&store.StackItem{ProfileID: profiles[0].ID, Category: "VPN", Choice: "Mine"})
	f.post("/logout", nil, nil)

	other := f.seedUser("other@example.com", "secret-password")
	f.login(other.Email, "secret-password")

	code, _, _ := f.patch("/stack_items/"+itoa64(item.ID), url.Values{"stack_item[choice]": {"Stolen"}}, nil)
	if code != http.StatusNotFound {
		t.Fatalf("PATCH foreign item = %d, want 404", code)
	}
	fresh, _ := f.store.FindOwnedStackItem(u.ID, item.ID)
	if fresh.Choice != "Mine" {
		t.Fatalf("choice = %q", fresh.Choice)
	}
	code, _, _ = f.get("/stack_items/"+itoa64(item.ID)+"/icon", nil)
	if code != http.StatusNotFound {
		t.Fatalf("GET foreign icon = %d, want 404", code)
	}
}

func TestSwitchingProfilesOnStackStaysOnStack(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	work, _ := f.store.CreateProfile(u.ID, "Work", 1)
	_, _ = f.store.CreateStackItem(&store.StackItem{ProfileID: work.ID, Category: "Email", Choice: "Mailcow", Origin: "Germany"})

	code, body, _ := f.get("/stack?profile_id="+itoa64(work.ID), nil)
	if code != http.StatusOK {
		t.Fatalf("GET /stack = %d", code)
	}
	mustContain(t, body, "Mailcow")
	mustContain(t, body, `class="profile-tab is-on">Work<`)
	mustContain(t, body, "/stack?profile_id="+itoa64(work.ID))
}

func TestServiceWorkerCachesAndLogoutWipes(t *testing.T) {
	f := newFlow(t, nil)
	// Serve the real sw.js out of a seeded WebDir.
	raw, err := os.ReadFile("../../web/static/sw.js")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sw.js"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	f.srv.WebDir = dir

	code, body, _ := f.get("/service-worker", nil)
	if code != http.StatusOK {
		t.Fatalf("GET /service-worker = %d", code)
	}
	mustContain(t, body, `const CACHE = "kurahome-v2"`)
	mustContain(t, body, "cache.put")

	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	code, body, _ = f.get("/", nil)
	if code != http.StatusOK {
		t.Fatalf("GET / = %d", code)
	}
	mustContain(t, body, `data-controller="logout"`)
}
