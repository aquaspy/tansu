package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aquasp/kuranotes/internal/config"
	"github.com/aquasp/kuranotes/internal/store"
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

func (f *flow) apiCall(method, path, token, body string) (int, map[string]any) {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, f.server.URL+path, reader)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
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

func mustContain(t *testing.T, body, want string) {
	t.Helper()
	if !strings.Contains(body, want) {
		t.Fatalf("body missing %q", want)
	}
}

func TestAuthFlow(t *testing.T) {
	f := newFlow(t, nil)
	code, _, h := f.post("/signup", url.Values{
		"email": {"you@example.com"}, "password": {"password1"}, "password_confirmation": {"password1"},
	}, nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/" {
		t.Fatalf("signup: %d -> %q", code, h.Get("Location"))
	}
	code, body, _ := f.get("/", nil)
	if code != http.StatusOK {
		t.Fatalf("index: %d", code)
	}
	mustContain(t, body, "TansuNotes")

	code, _, _ = f.post("/logout", nil, map[string]string{"HX-Request": "true"})
	if code != http.StatusOK {
		t.Fatalf("logout: %d", code)
	}
	code, _, h = f.get("/", nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/login" {
		t.Fatalf("after logout: %d -> %q", code, h.Get("Location"))
	}

	code, body, _ = f.post("/login", url.Values{"email": {"you@example.com"}, "password": {"wrongpass"}}, nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("bad login: %d", code)
	}
	mustContain(t, body, "Invalid email or password")
	f.login("you@example.com", "password1")

	closed := newFlow(t, func(c *config.Config) { c.SignupEnabled = false })
	code, _, h = closed.post("/signup", url.Values{
		"email": {"n@example.com"}, "password": {"password1"}, "password_confirmation": {"password1"},
	}, nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/login" {
		t.Fatalf("closed signup: %d -> %q", code, h.Get("Location"))
	}
}

func TestSessionCookiePersistent(t *testing.T) {
	f := newFlow(t, nil)
	f.seedUser("persist@example.com", "password1")
	code, _, h := f.post("/login", url.Values{"email": {"persist@example.com"}, "password": {"password1"}}, nil)
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

func TestLockFlow(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("you@example.com", "password1")
	f.login(u.Email, "password1")

	// Auto-lock on: lock the session, unlock requires the password.
	u2, _ := url.Parse(f.server.URL)
	f.client.Jar.SetCookies(u2, []*http.Cookie{{Name: AutoLockCookie, Value: "1"}})
	code, _, h := f.post("/lock", nil, nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/unlock" {
		t.Fatalf("lock: %d -> %q", code, h.Get("Location"))
	}
	code, body, _ := f.post("/unlock", url.Values{"password": {"nope-nope"}}, nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("bad unlock: %d", code)
	}
	mustContain(t, body, "Wrong password")
	code, _, h = f.post("/unlock", url.Values{"password": {"password1"}}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("unlock: %d", code)
	}
	code, _, _ = f.get("/", nil)
	if code != http.StatusOK {
		t.Fatalf("index after unlock: %d", code)
	}
}

func TestNotesWebFlow(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("you@example.com", "password1")
	f.login(u.Email, "password1")

	// Create opens a draft and redirects to it.
	code, _, h := f.post("/notes/", url.Values{"folder": {"work"}}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("create: %d", code)
	}
	loc := h.Get("Location")
	if !strings.HasPrefix(loc, "/notes/") {
		t.Fatalf("create redirect: %q", loc)
	}
	id := strings.TrimPrefix(strings.SplitN(loc, "?", 2)[0], "/notes/")

	// Autosave via htmx: OOB list + folders + picker.
	code, body, _ := f.methodCall(http.MethodPatch, "/notes/"+id,
		url.Values{"body": {"Trip ideas\nKyoto"}, "folder": {"travel"}}, map[string]string{"HX-Request": "true"})
	if code != http.StatusOK {
		t.Fatalf("autosave: %d", code)
	}
	for _, want := range []string{`hx-swap-oob="outerHTML:#notes_list"`, `hx-swap-oob="outerHTML:#folders_nav"`, "Trip ideas", "travel"} {
		mustContain(t, body, want)
	}

	// Plain update redirects back to the note.
	code, _, h = f.methodCall(http.MethodPatch, "/notes/"+id,
		url.Values{"body": {"Trip ideas\nKyoto\nOsaka"}, "folder": {"travel"}, "filter_folder": {"all"}}, nil)
	if code != http.StatusSeeOther || !strings.HasPrefix(h.Get("Location"), "/notes/"+id) {
		t.Fatalf("update: %d -> %q", code, h.Get("Location"))
	}

	// htmx search returns the list fragment only.
	code, body, _ = f.get("/notes/?q=kyoto", map[string]string{"HX-Request": "true"})
	if code != http.StatusOK {
		t.Fatalf("search: %d", code)
	}
	mustContain(t, body, `id="notes_list"`)
	if strings.Contains(body, "<!DOCTYPE") {
		t.Fatal("search returned a full page")
	}

	// Rename the folder.
	code, _, h = f.methodCall(http.MethodPatch, "/notes/folder",
		url.Values{"folder": {"travel"}, "name": {"trips"}}, map[string]string{"HX-Request": "true"})
	if code != http.StatusOK || h.Get("HX-Redirect") == "" {
		t.Fatalf("rename: %d redirect=%q", code, h.Get("HX-Redirect"))
	}
	rows, _ := f.store.ListNotes(u.ID, "trips", "")
	if len(rows) != 1 {
		t.Fatalf("renamed list: %+v", rows)
	}
	// Reserved rename is rejected with a flash + redirect.
	code, _, _ = f.methodCall(http.MethodPatch, "/notes/folder",
		url.Values{"folder": {"trips"}, "name": {"inbox"}}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("bad rename: %d", code)
	}
	code, body, _ = f.get("/notes/", nil)
	mustContain(t, body, "That folder name can&#39;t be used.")

	// Delete the note via fetch-style DELETE.
	code, _, _ = f.methodCall(http.MethodDelete, "/notes/"+id, nil, map[string]string{"X-Requested-With": "fetch"})
	if code != http.StatusOK {
		t.Fatalf("delete: %d", code)
	}
	if got := f.store.CountNotes(u.ID); got != 0 {
		t.Fatalf("notes left: %d", got)
	}
}

func TestFolderClear(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("you@example.com", "password1")
	f.login(u.Email, "password1")
	if _, err := f.store.CreateNote(u.ID, "a", "work"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateNote(u.ID, "b", ""); err != nil {
		t.Fatal(err)
	}
	code, _, _ := f.methodCall(http.MethodDelete, "/notes/folder?folder=work", nil,
		map[string]string{"X-Requested-With": "fetch"})
	if code != http.StatusOK {
		t.Fatalf("clear: %d", code)
	}
	if got := f.store.CountNotes(u.ID); got != 1 {
		t.Fatalf("notes left: %d", got)
	}
	code, _, h := f.methodCall(http.MethodDelete, "/notes/folder?folder=all", nil, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("clear all guard: %d -> %q", code, h.Get("Location"))
	}
}

func TestExportImport(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("you@example.com", "password1")
	f.login(u.Email, "password1")
	if _, err := f.store.CreateNote(u.ID, "Exported\nbody", "work"); err != nil {
		t.Fatal(err)
	}

	code, body, h := f.get("/notes/export", nil)
	if code != http.StatusOK {
		t.Fatalf("export: %d", code)
	}
	if !strings.Contains(h.Get("Content-Disposition"), "kuranotes-") {
		t.Fatalf("disposition: %q", h.Get("Content-Disposition"))
	}
	var payload []map[string]string
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 1 || payload[0]["title"] != "Exported" || payload[0]["folder"] != "work" || payload[0]["updated_at"] == "" {
		t.Fatalf("payload: %+v", payload)
	}

	// Import that export plus a text file.
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("csrf_token", f.csrf())
	fw, _ := w.CreateFormFile("file", "kuranotes.json")
	_, _ = fw.Write([]byte(body))
	fw, _ = w.CreateFormFile("file", "note.txt")
	_, _ = fw.Write([]byte("Hello import"))
	w.Close()
	req, _ := http.NewRequest(http.MethodPost, f.server.URL+"/notes/import", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("import: %d", resp.StatusCode)
	}
	if got := f.store.CountNotes(u.ID); got != 3 {
		t.Fatalf("notes after import: %d", got)
	}
	code, body, _ = f.get("/notes/", nil)
	mustContain(t, body, "Imported 2 notes.")

	// Garbage import shows the invalid alert.
	buf.Reset()
	w = multipart.NewWriter(&buf)
	_ = w.WriteField("csrf_token", f.csrf())
	fw, _ = w.CreateFormFile("file", "x.json")
	_, _ = fw.Write([]byte(`{"nope":true}`))
	w.Close()
	req, _ = http.NewRequest(http.MethodPost, f.server.URL+"/notes/import", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err = f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("bad import: %d", resp.StatusCode)
	}
	_, body, _ = f.get("/notes/", nil)
	mustContain(t, body, "not a valid TansuNotes")
}

func TestShares(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("you@example.com", "password1")
	f.login(u.Email, "password1")
	n, err := f.store.CreateNote(u.ID, "Shared title\nShared body", "")
	if err != nil {
		t.Fatal(err)
	}
	notePath := "/notes/" + strconv.FormatInt(n.ID, 10)

	code, body, _ := f.post(notePath+"/share", nil, map[string]string{"HX-Request": "true"})
	if code != http.StatusOK {
		t.Fatalf("share create: %d", code)
	}
	mustContain(t, body, "hx-swap-oob")
	n, _ = f.store.FindNote(u.ID, n.ID)
	if !n.Shared() {
		t.Fatal("note not shared")
	}

	// Anonymous read.
	anon := &http.Client{}
	resp, err := anon.Get(f.server.URL + "/s/" + n.ShareToken)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("shared page: %d", resp.StatusCode)
	}
	if !strings.Contains(string(raw), "Shared body") {
		t.Fatal("shared body missing")
	}
	if resp.Header.Get("X-Robots-Tag") == "" {
		t.Fatal("robots tag missing")
	}

	// Rotate, then remove.
	first := n.ShareToken
	code, _, _ = f.methodCall(http.MethodPatch, notePath+"/share", nil, map[string]string{"HX-Request": "true"})
	if code != http.StatusOK {
		t.Fatalf("share rotate: %d", code)
	}
	n, _ = f.store.FindNote(u.ID, n.ID)
	if n.ShareToken == first {
		t.Fatal("token not rotated")
	}
	code, _, _ = f.methodCall(http.MethodDelete, notePath+"/share", nil, map[string]string{"HX-Request": "true"})
	if code != http.StatusOK {
		t.Fatalf("share remove: %d", code)
	}
	resp, _ = anon.Get(f.server.URL + "/s/" + n.ShareToken)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("removed share: %d", resp.StatusCode)
	}
}

func TestTokensPage(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("you@example.com", "password1")
	f.login(u.Email, "password1")

	code, body, _ := f.get("/api_tokens/", nil)
	if code != http.StatusOK {
		t.Fatalf("tokens index: %d", code)
	}
	mustContain(t, body, "API tokens")

	code, body, _ = f.post("/api_tokens/", url.Values{"name": {"x"}, "current_password": {"wrongpass"}}, nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("wrong password: %d", code)
	}
	mustContain(t, body, "Wrong password")

	code, _, _ = f.post("/api_tokens/", url.Values{"name": {""}, "current_password": {"password1"}}, nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("blank name: %d", code)
	}

	code, body, _ = f.post("/api_tokens/", url.Values{"name": {"Hermes"}, "current_password": {"password1"}}, nil)
	if code != http.StatusCreated {
		t.Fatalf("create: %d", code)
	}
	if !strings.Contains(body, "kura_") {
		t.Fatal("raw token not shown")
	}
	// Shown once: reload and the full-token block is gone (only the
	// kura_… prefix stays in the list).
	_, body, _ = f.get("/api_tokens/", nil)
	if strings.Contains(body, "token-value") {
		t.Fatal("raw token shown twice")
	}
	mustContain(t, body, "Hermes")

	list, _ := f.store.ListTokens(u.ID)
	code, _, h := f.post("/api_tokens/"+strconv.FormatInt(list[0].ID, 10)+"/delete", nil, nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/api_tokens/" {
		t.Fatalf("revoke: %d -> %q", code, h.Get("Location"))
	}
	_, body, _ = f.get("/api_tokens/", nil)
	mustContain(t, body, "Token revoked.")
}
