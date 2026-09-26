package handler

import (
	"bytes"
	"encoding/json"
	"errors"
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

	"github.com/aquasp/kuraspend/internal/config"
	"github.com/aquasp/kuraspend/internal/store"
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
	mustContain(t, body, "TansuSpend")
	mustContain(t, body, `class="hero"`)

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
	code, body, _ = closed.get("/signup", nil)
	if code != http.StatusSeeOther {
		t.Fatalf("closed signup page: %d", code)
	}
	_ = body
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
	code, _, _ = f.post("/unlock", url.Values{"password": {"password1"}}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("unlock: %d", code)
	}
	code, _, _ = f.get("/", nil)
	if code != http.StatusOK {
		t.Fatalf("index after unlock: %d", code)
	}
}

func TestLoginOpensMonth(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	code, body, _ := f.get("/", nil)
	if code != http.StatusOK {
		t.Fatalf("month: %d", code)
	}
	mustContain(t, body, "TansuSpend")
	mustContain(t, body, `class="hero"`)
	mustContain(t, body, `data-leftover="0"`)
}

func TestStrangersCannotReadSpend(t *testing.T) {
	f := newFlow(t, nil)
	for _, path := range []string{"/", "/2026/8", "/export", "/api_tokens/"} {
		code, _, h := f.get(path, nil)
		if code != http.StatusSeeOther || h.Get("Location") != "/login" {
			t.Fatalf("GET %s: %d -> %q", path, code, h.Get("Location"))
		}
	}
}

func TestPreviousMonthRenders(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	code, body, _ := f.get("/2026/7", nil)
	if code != http.StatusOK {
		t.Fatalf("prev month: %d", code)
	}
	mustContain(t, body, `class="hero"`)
	mustContain(t, body, "July 2026")
}

func TestInvalidMonthFallsBackToCurrent(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	code, body, _ := f.get("/2026/13", nil)
	if code != http.StatusOK {
		t.Fatalf("bad month: %d", code)
	}
	mustContain(t, body, `class="hero"`)
}

func TestSalarySubsExpensesLeftover(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")

	code, _, h := f.methodCall(http.MethodPatch, "/settings", url.Values{
		"year": {"2026"}, "month": {"8"},
		"user[monthly_income]": {"10000"}, "user[home_currency]": {"BRL"}, "user[income_currency]": {"BRL"},
		"fx[USD]": {"5.45"},
	}, nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/2026/8" {
		t.Fatalf("settings: %d -> %q", code, h.Get("Location"))
	}
	code, _, _ = f.post("/subscriptions", url.Values{
		"year": {"2026"}, "month": {"8"},
		"subscription[title]": {"Netflix"}, "subscription[amount]": {"50"},
		"subscription[currency]": {"BRL"}, "subscription[interval]": {"monthly"},
	}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("subscription: %d", code)
	}
	code, _, _ = f.post("/payment_days", url.Values{
		"year": {"2026"}, "month": {"8"},
		"payment_day[title]": {"Water"}, "payment_day[due_day]": {"10"},
	}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("payment day: %d", code)
	}
	code, _, _ = f.post("/expenses", url.Values{
		"year": {"2026"}, "month": {"8"},
		"expense[title]": {"Coffee"}, "expense[amount]": {"15"},
		"expense[currency]": {"BRL"}, "expense[spent_on]": {"2026-08-12"},
	}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("expense: %d", code)
	}

	code, body, _ := f.get("/2026/8", nil)
	if code != http.StatusOK {
		t.Fatalf("month: %d", code)
	}
	for _, want := range []string{"Netflix", "Water", "Coffee", `data-leftover="993500"`} {
		mustContain(t, body, want)
	}

	// Bad settings show the validation flash, not a 500.
	code, _, _ = f.methodCall(http.MethodPatch, "/settings", url.Values{
		"year": {"2026"}, "month": {"8"},
		"user[monthly_income]": {"10000"}, "user[home_currency]": {"BRL"}, "user[income_currency]": {"BRL"},
		"fx[USD]": {"bogus"},
	}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("bad settings: %d", code)
	}
	_, body, _ = f.get("/2026/8", nil)
	mustContain(t, body, "Exchange rate must be a positive number.")
	_ = u
}

type stubRoundTripper struct{ dollar, euro string }

func (s stubRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	body := s.dollar
	if strings.Contains(r.URL.Path, "euro") {
		body = s.euro
	}
	return &http.Response{StatusCode: http.StatusOK, Status: "200 OK",
		Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{},
		Request: r}, nil
}

type errRoundTripper struct{}

func (errRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errFakeNetwork
}

var errFakeNetwork = errors.New("fx test: offline")

const (
	fxDollarPage = `<div id="cotacao"><span class="symbol">US$</span><input type="text" id="nacional" value="5,19"/></div>`
	fxEuroPage   = `<div id="cotacao"><span class="symbol">€</span><input type="text" id="nacional" value="5,91"/></div>`
)

func TestFXRefreshFlow(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("fx@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	f.srv.FXClient = &http.Client{Transport: stubRoundTripper{fxDollarPage, fxEuroPage}}

	code, _, h := f.post("/fx/refresh", url.Values{"year": {"2026"}, "month": {"8"}}, nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/2026/8" {
		t.Fatalf("refresh: %d -> %q", code, h.Get("Location"))
	}
	rows, err := f.store.FXQuotes()
	if err != nil || rows["USD"].Rate != "5.19" || rows["EUR"].Rate != "5.91" {
		t.Fatalf("cached: %+v, %v", rows, err)
	}
	_, body, _ := f.get("/2026/8", nil)
	mustContain(t, body, "Rates updated.")
	mustContain(t, body, "Live: USD 5.19 · EUR 5.91")

	// Auto rate converts with no manual rate ever set.
	code, _, _ = f.post("/expenses", url.Values{
		"year": {"2026"}, "month": {"8"},
		"expense[title]": {"Book"}, "expense[amount]": {"10"},
		"expense[currency]": {"USD"}, "expense[spent_on]": {"2026-08-12"},
	}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("expense: %d", code)
	}
	_, body, _ = f.get("/2026/8", nil)
	mustContain(t, body, "R$51.90")
	if strings.Contains(body, "Set a rate for") {
		t.Fatal("missing-rate banner shown despite fresh auto rates")
	}

	// A dead network fails loud but keeps the old cache working.
	f.srv.FXClient = &http.Client{Transport: errRoundTripper{}}
	if code, _, _ := f.post("/fx/refresh", url.Values{"year": {"2026"}, "month": {"8"}}, nil); code != http.StatusSeeOther {
		t.Fatalf("failed refresh: %d", code)
	}
	_, body, _ = f.get("/2026/8", nil)
	mustContain(t, body, "Could not fetch live rates.")
	mustContain(t, body, "R$51.90")
	_ = u
}

func TestExpenseWebCRUD(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")

	code, _, h := f.post("/expenses", url.Values{
		"year": {"2026"}, "month": {"8"},
		"expense[title]": {"Lunch"}, "expense[amount]": {"25.50"},
		"expense[currency]": {"BRL"}, "expense[spent_on]": {"2026-08-19"},
		"expense[category]": {"food"},
	}, nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/2026/8" {
		t.Fatalf("create: %d -> %q", code, h.Get("Location"))
	}
	list, _ := f.store.ListExpensesMonth(u.ID, "2026-08-01", "2026-08-31", "")
	if len(list) != 1 || list[0].AmountCents != 2550 {
		t.Fatalf("stored: %+v", list)
	}
	id := strconv.FormatInt(list[0].ID, 10)

	// Update via PATCH and via plain POST (dialog _method posts).
	code, _, _ = f.methodCall(http.MethodPatch, "/expenses/"+id, url.Values{
		"year": {"2026"}, "month": {"8"},
		"expense[title]": {"Lunch"}, "expense[amount]": {"30"},
		"expense[currency]": {"BRL"}, "expense[spent_on]": {"2026-08-19"},
		"expense[category]": {"leisure"},
	}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("patch: %d", code)
	}
	code, _, _ = f.post("/expenses/"+id, url.Values{
		"year": {"2026"}, "month": {"8"}, "_method": {"patch"},
		"expense[title]": {"Lunch"}, "expense[amount]": {"30"},
		"expense[currency]": {"BRL"}, "expense[spent_on]": {"2026-08-19"},
		"expense[category]": {"food"},
	}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("post update: %d", code)
	}
	// Failed validation keeps the row and flashes the errors.
	code, _, _ = f.methodCall(http.MethodPatch, "/expenses/"+id, url.Values{
		"year": {"2026"}, "month": {"8"},
		"expense[title]": {""}, "expense[amount]": {"30"},
		"expense[currency]": {"BRL"}, "expense[spent_on]": {"2026-08-19"},
	}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("bad update: %d", code)
	}
	_, body, _ := f.get("/2026/8", nil)
	mustContain(t, body, "Title can&#39;t be blank.")

	// Fetch-style DELETE returns 200 for the JS redirect.
	code, _, _ = f.methodCall(http.MethodDelete, "/expenses/"+id, nil,
		map[string]string{"X-Requested-With": "fetch"})
	if code != http.StatusOK {
		t.Fatalf("delete: %d", code)
	}
	if got := f.store.CountExpenses(u.ID); got != 0 {
		t.Fatalf("rows left: %d", got)
	}
	// Deleting again is a 404, like Rails find.
	code, _, _ = f.methodCall(http.MethodDelete, "/expenses/"+id, nil, nil)
	if code != http.StatusNotFound {
		t.Fatalf("second delete: %d", code)
	}
}

func TestSubscriptionAndDayWebFlow(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")

	code, _, _ := f.post("/subscriptions", url.Values{
		"subscription[title]": {"Music"}, "subscription[amount]": {"19.90"},
		"subscription[interval]": {"yearly"}, "subscription[billing_month]": {"3"},
	}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("sub create: %d", code)
	}
	subs, _ := f.store.ListSubscriptions(u.ID, false)
	if len(subs) != 1 || subs[0].Interval != "yearly" || *subs[0].BillingMonth != 3 {
		t.Fatalf("sub stored: %+v", subs)
	}
	sid := strconv.FormatInt(subs[0].ID, 10)
	code, _, _ = f.post("/subscriptions/"+sid+"/delete", nil, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("sub delete fallback: %d", code)
	}

	code, _, _ = f.post("/payment_days", url.Values{
		"payment_day[title]": {"Card"}, "payment_day[due_day]": {"31"},
	}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("day create: %d", code)
	}
	days, _ := f.store.ListPaymentDays(u.ID)
	did := strconv.FormatInt(days[0].ID, 10)
	code, _, _ = f.methodCall(http.MethodPatch, "/payment_days/"+did, url.Values{
		"payment_day[title]": {"Card"}, "payment_day[due_day]": {"28"},
	}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("day update: %d", code)
	}
	code, _, _ = f.methodCall(http.MethodDelete, "/payment_days/"+did, nil, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("day delete: %d", code)
	}
	if got := f.store.CountPaymentDays(u.ID); got != 0 {
		t.Fatalf("days left: %d", got)
	}
}

func TestAnotherUserCannotTouchExpense(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	title, amount, spent := "Secret", "5", "2026-08-25"
	e, _, err := f.store.CreateExpense(u.ID, store.ExpensePatch{
		Title: &title, Amount: &amount, SpentOn: &spent})
	if err != nil {
		t.Fatal(err)
	}
	other := f.seedUser("other@example.com", "secret-password")
	f.login(other.Email, "secret-password")
	stolen := "Stolen"
	code, _, _ := f.methodCall(http.MethodPatch, "/expenses/"+strconv.FormatInt(e.ID, 10),
		url.Values{"expense[title]": {stolen}}, nil)
	if code != http.StatusNotFound {
		t.Fatalf("cross-user update: %d", code)
	}
	current, _ := f.store.FindExpense(u.ID, e.ID)
	if current.Title != "Secret" {
		t.Fatalf("title changed: %q", current.Title)
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	title, amount := "Spotify", "34"
	cur, interval := "BRL", "monthly"
	if _, fails, err := f.store.CreateSubscription(u.ID, store.SubscriptionPatch{
		Title: &title, Amount: &amount, Currency: &cur, Interval: &interval}); err != nil || len(fails) > 0 {
		t.Fatalf("sub: %v %v", fails, err)
	}
	lunch, spent := "Lunch", "2026-08-20"
	lunchAmount := int64(2000)
	if _, fails, err := f.store.CreateExpense(u.ID, store.ExpensePatch{
		Title: &lunch, AmountCents: &lunchAmount, Currency: &cur, SpentOn: &spent}); err != nil || len(fails) > 0 {
		t.Fatalf("exp: %v %v", fails, err)
	}

	code, body, h := f.get("/export", nil)
	if code != http.StatusOK {
		t.Fatalf("export: %d", code)
	}
	if !strings.Contains(h.Get("Content-Disposition"), "kuraspend-") {
		t.Fatalf("disposition: %q", h.Get("Content-Disposition"))
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["app"] != "TansuSpend" {
		t.Fatalf("app: %v", payload["app"])
	}
	if len(payload["subscriptions"].([]any)) != 1 {
		t.Fatalf("subs: %v", payload["subscriptions"])
	}

	other := f.seedUser("other@example.com", "secret-password")
	f.login(other.Email, "secret-password")
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("csrf_token", f.csrf())
	fw, _ := w.CreateFormFile("file", "kuraspend.json")
	_, _ = fw.Write([]byte(body))
	w.Close()
	req, _ := http.NewRequest(http.MethodPost, f.server.URL+"/import", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("import: %d", resp.StatusCode)
	}
	if got := f.store.CountSubscriptions(other.ID); got != 1 {
		t.Fatalf("subs after import: %d", got)
	}
	if got := f.store.CountExpenses(other.ID); got != 1 {
		t.Fatalf("expenses after import: %d", got)
	}
	_, page, _ := f.get("/", nil)
	mustContain(t, page, "Imported 2 items.")

	// Garbage import shows the invalid alert.
	buf.Reset()
	w = multipart.NewWriter(&buf)
	_ = w.WriteField("csrf_token", f.csrf())
	fw, _ = w.CreateFormFile("file", "x.json")
	_, _ = fw.Write([]byte(`{"nope":true}`))
	w.Close()
	req, _ = http.NewRequest(http.MethodPost, f.server.URL+"/import", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err = f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("bad import: %d", resp.StatusCode)
	}
	_, page, _ = f.get("/", nil)
	mustContain(t, page, "not a valid TansuSpend export")
}

func TestServiceWorkerCache(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	// Serve the real file for this check.
	f.srv.WebDir = "../../web/static"
	code, body, _ := f.get("/service-worker", nil)
	if code != http.StatusOK {
		t.Fatalf("sw: %d", code)
	}
	mustContain(t, body, `const CACHE = "kuraspend-v2"`)
}

func TestTokensFlow(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")

	code, body, _ := f.get("/api_tokens/", nil)
	if code != http.StatusOK {
		t.Fatalf("tokens index: %d", code)
	}
	mustContain(t, body, "API tokens")

	code, body, _ = f.post("/api_tokens/", url.Values{"name": {"hermes"}, "current_password": {"wrong-password"}}, nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("wrong password: %d", code)
	}
	mustContain(t, body, "Wrong password")

	code, body, _ = f.post("/api_tokens/", url.Values{"name": {"hermes"}, "current_password": {"secret-password"}}, nil)
	if code != http.StatusCreated {
		t.Fatalf("create: %d", code)
	}
	// The raw token sits alone inside <code>.
	raw := ""
	if i := strings.Index(body, "kura_"); i >= 0 {
		j := i
		for j < len(body) && body[j] != '<' && body[j] != '"' && body[j] != ' ' {
			j++
		}
		raw = body[i:j]
	}
	if len(raw) <= 20 {
		t.Fatal("raw token not shown")
	}
	_, body, _ = f.get("/api_tokens/", nil)
	if strings.Contains(body, "token-value") {
		t.Fatal("raw token shown twice")
	}
	mustContain(t, body, "hermes")
	if code, _ := f.apiCall(http.MethodGet, "/api/v1/expenses", raw, ""); code != http.StatusOK {
		t.Fatalf("token api: %d", code)
	}

	// Revoking kills API access.
	list, _ := f.store.ListTokens(u.ID)
	code, _, h := f.methodCall(http.MethodDelete, "/api_tokens/"+strconv.FormatInt(list[0].ID, 10), nil, nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/api_tokens/" {
		t.Fatalf("revoke: %d -> %q", code, h.Get("Location"))
	}
	if code, _ := f.apiCall(http.MethodGet, "/api/v1/expenses", raw, ""); code != http.StatusUnauthorized {
		t.Fatalf("revoked api: %d", code)
	}

	// One user cannot revoke another user's token.
	other := f.seedUser("other@example.com", "secret-password")
	foreign, _, _ := f.store.CreateToken(other.ID, "other")
	code, _, _ = f.methodCall(http.MethodDelete, "/api_tokens/"+strconv.FormatInt(foreign.ID, 10), nil, nil)
	if code != http.StatusNotFound {
		t.Fatalf("foreign revoke: %d", code)
	}

	// Strangers bounce to login.
	code, _, _ = f.post("/logout", nil, nil)
	_ = code
	code, _, h = f.get("/api_tokens/", nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/login" {
		t.Fatalf("anon tokens: %d -> %q", code, h.Get("Location"))
	}
}

func TestPasswordChange(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")
	code, body, _ := f.get("/password/edit", nil)
	if code != http.StatusOK {
		t.Fatalf("edit: %d", code)
	}
	mustContain(t, body, "Change password")
	code, _, _ = f.methodCall(http.MethodPatch, "/password", url.Values{
		"current_password": {"wrong-password"}, "password": {"new-secret-1"}, "password_confirmation": {"new-secret-1"},
	}, nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("wrong current: %d", code)
	}
	code, _, h := f.methodCall(http.MethodPatch, "/password", url.Values{
		"current_password": {"secret-password"}, "password": {"new-secret-1"}, "password_confirmation": {"new-secret-1"},
	}, nil)
	if code != http.StatusSeeOther || h.Get("Location") != "/" {
		t.Fatalf("update: %d -> %q", code, h.Get("Location"))
	}
	_, body, _ = f.get("/", nil)
	mustContain(t, body, "Password changed.")
}
