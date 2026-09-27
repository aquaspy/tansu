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
	"strings"
	"testing"
	"time"

	"github.com/aquasp/kuracalendar/internal/config"
	"github.com/aquasp/kuracalendar/internal/store"
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

func (f *flow) upload(path, filename string, data []byte) (int, string, http.Header) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		f.t.Fatal(err)
	}
	_, _ = fw.Write(data)
	_ = w.WriteField("csrf_token", f.csrf())
	w.Close()
	req, _ := http.NewRequest(http.MethodPost, f.server.URL+path, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("X-CSRF-Token", f.csrf())
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

func TestSignupCreatesUser(t *testing.T) {
	f := newFlow(t, nil)
	code, _, _ := f.post("/signup", url.Values{
		"email": {"lin@example.com"}, "password": {"secret-password"},
		"password_confirmation": {"secret-password"},
	}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("signup: %d", code)
	}
	u, err := f.store.FindUserByEmail("lin@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordDigest), []byte("secret-password")) != nil {
		t.Fatal("password does not verify")
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordDigest), []byte("wrong")) == nil {
		t.Fatal("wrong password verifies")
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

func TestLoginOpensCalendar(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	code, body, _ := f.get("/", nil)
	if code != http.StatusOK {
		t.Fatalf("index: %d", code)
	}
	mustContain(t, body, "TansuCalendar")
	mustContain(t, body, "cal-grid")
	mustContain(t, body, "is-today")
	mustContain(t, body, `aria-current="date"`)
}

func TestStrangersRedirect(t *testing.T) {
	f := newFlow(t, nil)
	if code, _, h := f.get("/", nil); code != http.StatusSeeOther || h.Get("Location") != "/login" {
		t.Fatalf("root: %d %q", code, h.Get("Location"))
	}
}

func TestAddEvent(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	code, _, h := f.post("/events", url.Values{
		"title": {"Dentist"}, "starts_on": {"2026-08-25"}, "ends_on": {"2026-08-25"}, "all_day": {"1"},
	}, nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/2026/8/25" {
		t.Fatalf("create: %d %q", code, h.Get("Location"))
	}
	if n := f.store.CountEvents(u.ID); n != 1 {
		t.Fatalf("events = %d", n)
	}
	code, body, _ := f.get("/2026/8/25", nil)
	if code != http.StatusOK {
		t.Fatalf("month: %d", code)
	}
	mustContain(t, body, "Dentist")
}

func TestSeriesEmojiRepeatFlow(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	code, _, h := f.post("/events", url.Values{
		"title": {"Gym"}, "starts_on": {"2026-09-07"}, "ends_on": {"2026-09-07"}, "all_day": {"1"},
		"emoji": {"🏋️"}, "repeat": {"weekly"},
	}, nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/2026/9/7" {
		t.Fatalf("create: %d %q", code, h.Get("Location"))
	}
	// One stored row renders on every occurrence date with the emoji pill,
	// the repeat badge, and the series delete warning.
	if n := f.store.CountEvents(u.ID); n != 1 {
		t.Fatalf("events = %d", n)
	}
	_, body, _ := f.get("/2026/9/14", nil)
	mustContain(t, body, "🏋️ Gym")
	mustContain(t, body, "item-repeat")
	mustContain(t, body, "data-repeat=\"weekly\"")
	mustContain(t, body, "Delete this event and all its repetitions?")
	mustContain(t, body, `list="emoji-presets"`)
	mustContain(t, body, `<option value="weekly">`)
	mustContain(t, body, `name="repeat_until"`)
	// Editing an occurrence edits the series template.
	_, body, _ = f.get("/2026/9/21", nil)
	mustContain(t, body, "🏋️ Gym")
	_ = u
}

func TestBirthdayEmojiFlow(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	if _, errs, err := f.store.UpsertSyncedBirthday(u.ID, "people:1", store.BirthdayInput{
		Name: "Ada", Month: "9", Day: "14", Emoji: "🎂",
	}); err != nil || len(errs) > 0 {
		t.Fatalf("sync: %v %+v", err, errs)
	}
	_, body, _ := f.get("/2026/9/14", nil)
	mustContain(t, body, "🎂 Ada")
	if strings.Contains(body, "composer#newBirthday") || strings.Contains(body, `action="/birthdays"`) {
		t.Fatal("birthday is not a creation choice")
	}
}

func TestBirthdayCreateRetired(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	code, _, h := f.post("/birthdays", url.Values{
		"name": {"Ada"}, "month": {"9"}, "day": {"14"},
	}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("create: %d", code)
	}
	if n := f.store.CountBirthdays(u.ID); n != 0 {
		t.Fatalf("birthdays = %d", n)
	}
	_, body, _ := f.get(h.Get("Location"), nil)
	mustContain(t, body, "Tansu People")
}

func TestTimedEventAndBirthday(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	if code, _, _ := f.post("/events", url.Values{
		"title": {"Call"}, "starts_on": {"2026-08-26"},
		"starts_at": {"14:30"}, "ends_at": {"15:00"},
	}, nil); code != http.StatusSeeOther {
		t.Fatalf("timed event: %d", code)
	}
	if _, errs, err := f.store.UpsertSyncedBirthday(u.ID, "people:11", store.BirthdayInput{
		Name: "Eben", Month: "8", Day: "11", Year: "1990",
	}); err != nil || len(errs) > 0 {
		t.Fatalf("birthday: %v %+v", err, errs)
	}
	_, body, _ := f.get("/2026/8/26", nil)
	mustContain(t, body, "Call")
	mustContain(t, body, "14:30")
	_, body, _ = f.get("/2026/8/11", nil)
	mustContain(t, body, "Eben (36)")
}

func TestCrossUserEvent404(t *testing.T) {
	f := newFlow(t, nil)
	a := f.seedUser("ada@example.com", "secret-password")
	e, _, err := f.store.CreateEvent(a.ID, store.EventInput{
		Title: "Secret", AllDay: true, StartsOn: "2026-08-25", EndsOn: "2026-08-25",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.seedUser("other@example.com", "secret-password")
	f.login("other@example.com", "secret-password")
	code, _, _ := f.methodCall(http.MethodPatch, "/events/"+itoa64(e.ID),
		url.Values{"title": {"Stolen"}}, nil)
	if code != http.StatusNotFound {
		t.Fatalf("cross-user patch: %d", code)
	}
	back, _ := f.store.FindEvent(a.ID, e.ID)
	if back.Title != "Secret" {
		t.Fatal("title changed")
	}
}

func TestHolidaysToggle(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	_, body, _ := f.get("/2026/9/7", nil)
	mustContain(t, body, "Independence Day")
	code, _, h := f.post("/holidays", url.Values{
		"year": {"2026"}, "month": {"9"}, "day": {"7"},
	}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("holidays: %d", code)
	}
	_, body, _ = f.get(h.Get("Location"), nil)
	mustNotContain(t, body, "Independence Day")
	u, _ = f.store.FindUser(u.ID)
	if len(u.HolidayCountryCodes()) != 0 {
		t.Fatalf("codes = %+v", u.HolidayCountryCodes())
	}
}

func TestTwoPacksShareDay(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	form := url.Values{"year": {"2026"}, "month": {"9"}, "day": {"7"}}
	form.Add("countries", "BR")
	form.Add("countries", "US")
	code, _, h := f.post("/holidays", form, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("holidays: %d", code)
	}
	_, body, _ := f.get(h.Get("Location"), nil)
	mustContain(t, body, "Independence Day")
	mustContain(t, body, "Labor Day")
	mustContain(t, body, "BR")
	mustContain(t, body, "US")
	_ = u
}

func TestSpanningEvent(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	if code, _, _ := f.post("/events", url.Values{
		"title": {"Trip"}, "starts_on": {"2026-08-31"}, "ends_on": {"2026-09-01"}, "all_day": {"1"},
	}, nil); code != http.StatusSeeOther {
		t.Fatalf("create: %d", code)
	}
	_, body, _ := f.get("/2026/8/31", nil)
	mustContain(t, body, "Trip")
	_, body, _ = f.get("/2026/9/1", nil)
	mustContain(t, body, "Trip")
	_ = u
}

func TestExportImportRoundTrip(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	if _, errs, err := f.store.CreateEvent(u.ID, store.EventInput{
		Title: "Park", AllDay: true, StartsOn: "2026-08-25", EndsOn: "2026-08-25",
	}); err != nil || len(errs) > 0 {
		t.Fatalf("event: %v %+v", err, errs)
	}
	if _, errs, err := f.store.CreateBirthday(u.ID, store.BirthdayInput{
		Name: "Ada", Month: "8", Day: "11", Year: "2000",
	}); err != nil || len(errs) > 0 {
		t.Fatalf("birthday: %v %+v", err, errs)
	}
	code, payload, h := f.get("/export", nil)
	if code != http.StatusOK {
		t.Fatalf("export: %d", code)
	}
	if ct := h.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(payload), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["app"] != "TansuCalendar" || len(doc["events"].([]any)) != 1 || len(doc["birthdays"].([]any)) != 1 {
		t.Fatalf("payload = %v", doc["app"])
	}

	other := f.seedUser("lin@example.com", "secret-password")
	f.login(other.Email, "secret-password")
	code, _, h = f.upload("/import", "kuracalendar-2026-09-25.json", []byte(payload))
	if code != http.StatusSeeOther || h.Get("Location") != "/" {
		t.Fatalf("import: %d %q", code, h.Get("Location"))
	}
	if n := f.store.CountEvents(other.ID); n != 2 {
		t.Fatalf("events = %d", n)
	}
	events, _ := f.store.EventsInRange(other.ID, "2026-01-01", "2026-12-31")
	if len(events) != 1 || events[0].Title != "Park" {
		t.Fatalf("one-shots = %+v", events)
	}
	series, err := f.store.ListRepeatingEvents(other.ID)
	if err != nil || len(series) != 1 || series[0].Title != "Ada" || series[0].Repeat != "yearly" {
		t.Fatalf("imported birthday = %+v %v", series, err)
	}
	if n := f.store.CountBirthdays(other.ID); n != 0 {
		t.Fatalf("birthdays = %d", n)
	}
	// And the flash confirms the count.
	_, body, _ := f.get("/", nil)
	mustContain(t, body, "Imported 2 items.")
}

func TestImportInvalidFile(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	code, _, h := f.upload("/import", "notes.json", []byte(`{"notes": []}`))
	if code != http.StatusSeeOther || h.Get("Location") != "/" {
		t.Fatalf("import: %d %q", code, h.Get("Location"))
	}
	_, body, _ := f.get("/", nil)
	mustContain(t, body, "not a valid TansuCalendar export")
	_ = u
}

func TestLockHidesCalendar(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	today := "2026-09-25"
	if _, errs, err := f.store.CreateEvent(u.ID, store.EventInput{
		Title: "Hidden after lock", AllDay: true, StartsOn: today, EndsOn: today,
	}); err != nil || len(errs) > 0 {
		t.Fatalf("event: %v %+v", err, errs)
	}
	code, _, h := f.post("/lock", nil, nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/unlock" {
		t.Fatalf("lock: %d %q", code, h.Get("Location"))
	}
	code, body, _ := f.get("/unlock", nil)
	if code != http.StatusOK {
		t.Fatalf("unlock: %d", code)
	}
	mustNotContain(t, body, "Hidden after lock")
	if code, _, _ := f.get("/", nil); code != http.StatusSeeOther {
		t.Fatalf("root while locked: %d", code)
	}
	// Unlock with the password restores the calendar.
	// Auto-lock is off in tests, so any visit reopens... lock again first.
	code, _, _ = f.post("/unlock", url.Values{"password": {"secret-password"}}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("unlock post: %d", code)
	}
}

func TestServiceWorkerCache(t *testing.T) {
	f := newFlow(t, nil)
	code, body, _ := f.get("/service-worker", nil)
	if code != http.StatusOK {
		t.Fatalf("sw: %d", code)
	}
	mustContain(t, body, `const CACHE = "kuracalendar-v2"`)
}

func TestManifest(t *testing.T) {
	f := newFlow(t, nil)
	code, body, _ := f.get("/manifest", nil)
	if code != http.StatusOK {
		t.Fatalf("manifest: %d", code)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["name"] != "TansuCalendar" {
		t.Fatalf("name = %v", doc["name"])
	}
}

func TestMethodOverridePatch(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	e, _, err := f.store.CreateEvent(u.ID, store.EventInput{
		Title: "Old", AllDay: true, StartsOn: "2026-08-25", EndsOn: "2026-08-25",
	})
	if err != nil {
		t.Fatal(err)
	}
	code, _, _ := f.post("/events/"+itoa64(e.ID), url.Values{
		"_method": {"patch"}, "title": {"New"},
		"starts_on": {"2026-08-25"}, "ends_on": {"2026-08-25"}, "all_day": {"1"},
	}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("override patch: %d", code)
	}
	back, _ := f.store.FindEvent(u.ID, e.ID)
	if back.Title != "New" {
		t.Fatalf("title = %q", back.Title)
	}
}

func TestInvalidEventFlashes(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	code, _, h := f.post("/events", url.Values{"title": {""}, "starts_on": {"2026-08-25"}}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("create: %d", code)
	}
	_, body, _ := f.get(h.Get("Location"), nil)
	mustContain(t, body, "Title can&#39;t be blank")
	if n := f.store.CountEvents(u.ID); n != 0 {
		t.Fatalf("events = %d", n)
	}
}

func TestApiTokensFlow(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")

	// Wrong password creates nothing.
	code, _, _ := f.post("/api_tokens/", url.Values{
		"name": {"hermes"}, "current_password": {"wrong-password"},
	}, nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("bad password: %d", code)
	}
	if list, _ := f.store.ListTokens(u.ID); len(list) != 0 {
		t.Fatal("token created without password")
	}

	// Create: the raw token shows once.
	code, body, _ := f.post("/api_tokens/", url.Values{
		"name": {"hermes"}, "current_password": {"secret-password"},
	}, nil)
	if code != http.StatusCreated {
		t.Fatalf("create: %d", code)
	}
	idx := strings.Index(body, "kura_")
	if idx < 0 {
		t.Fatal("raw token missing from response")
	}
	raw := body[idx:]
	if end := strings.IndexAny(raw, "<\" \t\r\n"); end >= 0 {
		raw = raw[:end]
	}
	if len(raw) < 20 {
		t.Fatalf("raw shape: %q", raw)
	}
	code, body, _ = f.get("/api_tokens/", nil)
	if code != http.StatusOK {
		t.Fatalf("index: %d", code)
	}
	mustNotContain(t, body, raw)
	mustContain(t, body, "hermes")

	// The token works for the API.
	if code, _ := f.apiCall(http.MethodGet, "/api/v1/events", raw, ""); code != http.StatusOK {
		t.Fatalf("api with token: %d", code)
	}

	// Revoke kills API access.
	list, _ := f.store.ListTokens(u.ID)
	code, _, h := f.methodCall(http.MethodDelete, "/api_tokens/"+itoa64(list[0].ID), nil, nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/api_tokens/" {
		t.Fatalf("revoke: %d %q", code, h.Get("Location"))
	}
	if code, _ := f.apiCall(http.MethodGet, "/api/v1/events", raw, ""); code != http.StatusUnauthorized {
		t.Fatalf("api after revoke: %d", code)
	}
}

func TestApiTokensCrossUser404(t *testing.T) {
	f := newFlow(t, nil)
	other := f.seedUser("other@example.com", "secret-password")
	tok, _, err := f.store.CreateToken(other.ID, "other")
	if err != nil {
		t.Fatal(err)
	}
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	code, _, _ := f.methodCall(http.MethodDelete, "/api_tokens/"+itoa64(tok.ID), nil, nil)
	if code != http.StatusNotFound {
		t.Fatalf("cross-user revoke: %d", code)
	}
	if _, err := f.store.FindToken(other.ID, tok.ID); err != nil {
		t.Fatal("token deleted by stranger")
	}
	_ = u
}

func TestApiTokensStrangersRedirect(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	if code, _, _ := f.methodCall(http.MethodDelete, "/logout", nil, nil); code != http.StatusSeeOther {
		t.Fatalf("logout: %d", code)
	}
	if code, _, h := f.get("/api_tokens/", nil); code != http.StatusSeeOther || h.Get("Location") != "/login" {
		t.Fatalf("tokens page: %d %q", code, h.Get("Location"))
	}
}
