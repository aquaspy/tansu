package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aquasp/kurapeople/internal/config"
	"github.com/aquasp/kurapeople/internal/store"
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
	srv.WebDir = "../../web/static"
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

func mustNotContain(t *testing.T, body, want string) {
	t.Helper()
	if strings.Contains(body, want) {
		t.Fatalf("body should not contain %q", want)
	}
}

func TestSignupClosed(t *testing.T) {
	f := newFlow(t, func(c *config.Config) { c.SignupEnabled = false })
	if code, _, h := f.get("/signup", nil); code != http.StatusSeeOther || h.Get("Location") != "/login" {
		t.Fatalf("signup page: %d %q", code, h.Get("Location"))
	}
	code, body, _ := f.get("/login", nil)
	if code != http.StatusOK {
		t.Fatalf("login: %d", code)
	}
	mustNotContain(t, body, "Create one")
	before, _ := f.store.ListUsers()
	code, _, h := f.post("/signup", url.Values{
		"email": {"intruder@example.com"}, "password": {"secret-password"},
		"password_confirmation": {"secret-password"},
	}, nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/login" {
		t.Fatalf("signup post: %d %q", code, h.Get("Location"))
	}
	after, _ := f.store.ListUsers()
	if len(after) != len(before) {
		t.Fatal("user created while signup closed")
	}
}

func TestIndexRequiresAuth(t *testing.T) {
	f := newFlow(t, nil)
	if code, _, h := f.get("/", nil); code != http.StatusSeeOther || h.Get("Location") != "/login" {
		t.Fatalf("index: %d %q", code, h.Get("Location"))
	}
}

func TestEmptyIndex(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	code, body, _ := f.get("/", nil)
	if code != http.StatusOK {
		t.Fatalf("index: %d", code)
	}
	mustContain(t, body, "Nobody here yet")
}

func personForm(name string) url.Values {
	return url.Values{
		"name": {name}, "nickname": {"Ada"}, "relationship": {"friend"},
		"birth_month": {"8"}, "birth_day": {"11"}, "birth_year": {"2000"},
		"emoji": {"🎂"}, "phone": {"+55 11 90000-0000"}, "height": {"1.65m"},
		"ring_size": {"16"}, "favorites": {"tea"},
		"attr_label": {"Coffee"}, "attr_value": {"flat white"},
	}
}

func TestPersonWebCRUD(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")

	code, _, h := f.post("/people", personForm("Ada Lovelace"), nil)
	if code != http.StatusSeeOther {
		t.Fatalf("create: %d", code)
	}
	detail := h.Get("Location")
	if !strings.HasPrefix(detail, "/people/") {
		t.Fatalf("create redirect: %q", detail)
	}

	code, body, _ := f.get(detail, nil)
	if code != http.StatusOK {
		t.Fatalf("show: %d", code)
	}
	for _, want := range []string{"Ada Lovelace", "friend", "August 11", "1.65m", "flat white", "tea"} {
		mustContain(t, body, want)
	}

	code, body, _ = f.get("/", nil)
	if code != http.StatusOK {
		t.Fatalf("index: %d", code)
	}
	mustContain(t, body, "Ada Lovelace")

	form := personForm("Ada Lovelace")
	form.Set("nickname", "A.")
	code, _, _ = f.methodCall(http.MethodPatch, detail, form, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("update: %d", code)
	}
	_, body, _ = f.get(detail, nil)
	mustContain(t, body, "A.")

	code, _, _ = f.methodCall(http.MethodDelete, detail, nil,
		map[string]string{"X-Requested-With": "fetch"})
	if code != http.StatusOK {
		t.Fatalf("delete: %d", code)
	}
	if code, _, _ := f.get(detail, nil); code != http.StatusNotFound {
		t.Fatalf("show after delete: %d", code)
	}
}

func TestPersonValidationFlashes(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")

	form := personForm("  ")
	code, _, h := f.post("/people", form, nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/" {
		t.Fatalf("create: %d %q", code, h.Get("Location"))
	}
	_, body, _ := f.get("/", nil)
	mustContain(t, body, "Name can&#39;t be blank")
}

func TestPersonSearchAndFilter(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	f.post("/people", personForm("Ada Lovelace"), nil)
	lin := personForm("Lin")
	lin.Set("relationship", "family")
	lin.Set("nickname", "")
	f.post("/people", lin, nil)

	_, body, _ := f.get("/?q=lovelace", nil)
	mustContain(t, body, "Ada Lovelace")
	mustNotContain(t, body, "🎂 Lin")

	_, body, _ = f.get("/?relationship=family", nil)
	mustContain(t, body, "🎂 Lin")
	mustNotContain(t, body, "Ada Lovelace")
}

func TestPersonScopedToOwner(t *testing.T) {
	f := newFlow(t, nil)
	a := f.seedUser("a@example.com", "secret-password")
	b := f.seedUser("b@example.com", "secret-password")
	p, _, err := f.store.CreatePerson(a.ID, store.PersonInput{Name: "Secret"})
	if err != nil {
		t.Fatal(err)
	}
	f.login(b.Email, "secret-password")
	path := "/people/" + itoa64(p.ID)
	if code, _, _ := f.get(path, nil); code != http.StatusNotFound {
		t.Fatalf("stranger show: %d", code)
	}
	_, body, _ := f.get("/", nil)
	mustNotContain(t, body, "Secret")
}

func TestUpcomingStrip(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	soon := time.Now().AddDate(0, 0, 5)
	form := personForm("Soon Person")
	form.Set("birth_month", strings.TrimPrefix(soon.Format("01"), "0"))
	form.Set("birth_day", strings.TrimPrefix(soon.Format("02"), "0"))
	form.Set("birth_year", "")
	f.post("/people", form, nil)
	_, body, _ := f.get("/", nil)
	mustContain(t, body, "Coming up")
	mustContain(t, body, "in 5 days")
}

func TestBirthdayICS(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	_, _, h := f.post("/people", personForm("Ada Lovelace"), nil)
	detail := h.Get("Location")
	dateless := personForm("No Date")
	dateless.Del("birth_month")
	dateless.Del("birth_day")
	dateless.Del("birth_year")
	f.post("/people", dateless, nil)

	code, body, hdr := f.get(detail+"/ics", nil)
	if code != http.StatusOK {
		t.Fatalf("person ics: %d", code)
	}
	if ct := hdr.Get("Content-Type"); !strings.Contains(ct, "text/calendar") {
		t.Fatalf("content type: %q", ct)
	}
	for _, want := range []string{"BEGIN:VEVENT", "RRULE:FREQ=YEARLY", "Ada Lovelace", "20000811"} {
		mustContain(t, body, want)
	}

	code, body, _ = f.get("/birthdays.ics", nil)
	if code != http.StatusOK {
		t.Fatalf("birthdays ics: %d", code)
	}
	mustContain(t, body, "Ada Lovelace")
	mustNotContain(t, body, "No Date")
}

func TestCalendarJSONExport(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	_, _, h := f.post("/people", personForm("Ada Lovelace"), nil)
	detail := h.Get("Location")
	dateless := personForm("No Date")
	dateless.Del("birth_month")
	dateless.Del("birth_day")
	dateless.Del("birth_year")
	f.post("/people", dateless, nil)

	code, body, hdr := f.get(detail+"/calendar.json", nil)
	if code != http.StatusOK {
		t.Fatalf("person json: %d", code)
	}
	if ct := hdr.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content type: %q", ct)
	}
	var one map[string]any
	if err := json.Unmarshal([]byte(body), &one); err != nil {
		t.Fatalf("json: %v", err)
	}
	if one["app"] != "TansuCalendar" {
		t.Fatalf("app marker: %+v", one)
	}
	rows := one["birthdays"].([]any)
	if len(rows) != 1 {
		t.Fatalf("rows: %+v", one)
	}
	row := rows[0].(map[string]any)
	if row["name"] != "Ada Lovelace" || row["month"] != 8.0 || row["day"] != 11.0 || row["year"] != 2000.0 {
		t.Fatalf("row: %+v", row)
	}

	code, body, _ = f.get("/birthdays.json", nil)
	if code != http.StatusOK {
		t.Fatalf("birthdays json: %d", code)
	}
	mustContain(t, body, "Ada Lovelace")
	mustNotContain(t, body, "No Date")
}

func TestTokensFlow(t *testing.T) {
	f := newFlow(t, nil)
	if code, _, _ := f.get("/api_tokens/", nil); code != http.StatusSeeOther {
		t.Fatalf("tokens anonymous: %d", code)
	}
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	code, body, _ := f.get("/api_tokens/", nil)
	if code != http.StatusOK {
		t.Fatalf("tokens index: %d", code)
	}
	mustContain(t, body, "API tokens")
	code, body, _ = f.post("/api_tokens/", url.Values{
		"name": {"hermes"}, "current_password": {"secret-password"},
	}, nil)
	if code != http.StatusCreated {
		t.Fatalf("tokens create: %d", code)
	}
	mustContain(t, body, "kura_")
}
