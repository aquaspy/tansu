package handler

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aquasp/kuraemail/internal/config"
	"github.com/aquasp/kuraemail/internal/mailtest"
	"github.com/aquasp/kuraemail/internal/store"
	"golang.org/x/crypto/bcrypt"
)

const testSecretsKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type flow struct {
	t      *testing.T
	server *httptest.Server
	client *http.Client
	store  *store.Store
}

func newFlow(t *testing.T) *flow {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	key, err := decodeTestKey(testSecretsKey)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{DataDir: t.TempDir(), SignupEnabled: true, SecretsKey: key}
	srv := NewServer(cfg, st)
	srv.WebDir = t.TempDir()
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	f := &flow{t: t, server: ts, client: client, store: st}
	_, _, _ = f.get("/login", nil)
	return f
}

func decodeTestKey(hexKey string) ([]byte, error) {
	raw := make([]byte, 32)
	for i := 0; i < 32; i++ {
		b, err := strconv.ParseUint(hexKey[i*2:i*2+2], 16, 8)
		if err != nil {
			return nil, err
		}
		raw[i] = byte(b)
	}
	return raw, nil
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

func (f *flow) post(path string, form url.Values) (int, string, http.Header) {
	if form == nil {
		form = url.Values{}
	}
	form.Set("csrf_token", f.csrf())
	req, _ := http.NewRequest(http.MethodPost, f.server.URL+path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
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
	code, _, _ := f.post("/login", url.Values{"email": {email}, "password": {password}})
	if code != http.StatusSeeOther {
		f.t.Fatalf("login status = %d", code)
	}
}

func (f *flow) apiCall(method, path, token, body string) (int, string, map[string]any) {
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
	return resp.StatusCode, string(raw), out
}

func TestSignupShell(t *testing.T) {
	f := newFlow(t)
	code, _, h := f.post("/signup", url.Values{
		"email": {"you@example.com"}, "password": {"password1"}, "password_confirmation": {"password1"},
	})
	if code != http.StatusSeeOther || h.Get("Location") != "/" {
		t.Fatalf("signup: %d -> %q", code, h.Get("Location"))
	}
	code, body, _ := f.get("/", nil)
	if code != http.StatusOK || !strings.Contains(body, "Tansu Email") || !strings.Contains(body, "No mailbox yet") {
		t.Fatalf("home: %d missing shell", code)
	}
	if !strings.Contains(body, "Connect mailbox") || !strings.Contains(body, "btn-new") || !strings.Contains(body, "/icon.png") {
		t.Fatal("missing connect mailbox call to action or wordmark")
	}
	code, body, _ = f.get("/", map[string]string{"Accept-Language": "pt-BR"})
	if code != http.StatusOK || !strings.Contains(body, "Conectar caixa") || !strings.Contains(body, "Nenhuma caixa ainda") {
		t.Fatalf("pt home: %d", code)
	}
}

func TestAPIMailboxRoundTrip(t *testing.T) {
	f := newFlow(t)
	u := f.seedUser("you@example.com", "password1")
	_, raw, err := f.store.CreateToken(u.ID, "agent")
	if err != nil {
		t.Fatal(err)
	}
	imap, err := mailtest.StartIMAP("me", "s3cret-mail")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(imap.Close)
	smtp, err := mailtest.StartSMTP("me", "s3cret-mail")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(smtp.Close)

	if code, _, out := f.apiCall(http.MethodGet, "/api/v1/accounts", "", ""); code != http.StatusUnauthorized || out["error"] != "unauthorized" {
		t.Fatalf("no token: %d %+v", code, out)
	}

	payload := `{
		"display_name":"Me",
		"from_address":"me@example.com",
		"username":"me",
		"password":"s3cret-mail",
		"imap_host":"` + imap.Host + `",
		"imap_port":` + strconv.Itoa(imap.Port) + `,
		"imap_tls":"none",
		"smtp_host":"` + smtp.Host + `",
		"smtp_port":` + strconv.Itoa(smtp.Port) + `,
		"smtp_tls":"none"
	}`
	code, body, out := f.apiCall(http.MethodPost, "/api/v1/accounts", raw, payload)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, body)
	}
	if strings.Contains(body, "s3cret-mail") || strings.Contains(body, "password_enc") {
		t.Fatalf("password leaked: %s", body)
	}
	acct := out["account"].(map[string]any)
	if acct["password_set"] != true || acct["last_ok"] == nil || acct["last_error"] != "" {
		t.Fatalf("status: %+v", acct)
	}
	id := strconv.FormatFloat(acct["id"].(float64), 'f', 0, 64)
	saved, err := f.store.FindMailbox(u.ID, int64(acct["id"].(float64)))
	if err != nil || strings.Contains(saved.PasswordEnc, "s3cret-mail") || saved.PasswordEnc == "" {
		t.Fatalf("ciphertext: %v %q", err, saved.PasswordEnc)
	}

	code, body, out = f.apiCall(http.MethodGet, "/api/v1/accounts/"+id+"/folders", raw, "")
	if code != http.StatusOK || !strings.Contains(body, "INBOX") {
		t.Fatalf("folders: %d %s", code, body)
	}
	code, body, out = f.apiCall(http.MethodGet, "/api/v1/accounts/"+id+"/messages?folder=INBOX", raw, "")
	if code != http.StatusOK {
		t.Fatalf("list: %d %s", code, body)
	}
	msgs := out["messages"].([]any)
	if len(msgs) != 2 || out["total"] != float64(2) {
		t.Fatalf("page: %+v", out)
	}
	first := msgs[0].(map[string]any)
	second := msgs[1].(map[string]any)
	if first["seen"] != true || second["seen"] != false || second["subject"] != "Quarterly update" {
		t.Fatalf("flags: %+v %+v", first, second)
	}
	code, _, out = f.apiCall(http.MethodGet, "/api/v1/accounts/"+id+"/messages?folder=INBOX&q=hello", raw, "")
	if code != http.StatusOK || len(out["messages"].([]any)) != 1 {
		t.Fatalf("search: %d %+v", code, out)
	}
	code, body, out = f.apiCall(http.MethodGet, "/api/v1/accounts/"+id+"/messages/11?folder=INBOX", raw, "")
	if code != http.StatusOK || !strings.Contains(body, "Hello from Ada") {
		t.Fatalf("read: %d %s", code, body)
	}
	code, body, out = f.apiCall(http.MethodPost, "/api/v1/accounts/"+id+"/preview", raw, `{"to":["bob@example.com"],"subject":"Hi","body":"There"}`)
	if code != http.StatusOK || out["sent"] != false || !strings.Contains(body, "There") {
		t.Fatalf("preview: %d %s", code, body)
	}
	if smtp.Data != "" {
		t.Fatal("preview sent mail")
	}
	code, body, out = f.apiCall(http.MethodPost, "/api/v1/accounts/"+id+"/send", raw, `{"to":["bob@example.com"],"subject":"Hi","text":"There"}`)
	if code != http.StatusOK || out["sent"] != true || !strings.Contains(smtp.Data, "Subject: Hi") || strings.Contains(smtp.Data, "s3cret-mail") {
		t.Fatalf("send: %d %s data %q", code, body, smtp.Data)
	}
	code, _, _ = f.apiCall(http.MethodPost, "/api/v1/accounts/"+id+"/messages/11/trash?folder=INBOX", raw, "")
	if code != http.StatusNoContent || len(imap.Moved) == 0 {
		t.Fatalf("trash: %d moved %v", code, imap.Moved)
	}

	f.login("you@example.com", "password1")
	code, body, _ = f.get("/?account="+id, nil)
	if code != http.StatusOK || !strings.Contains(body, "Inbox") || !strings.Contains(body, "Hello") {
		t.Fatalf("web list: %d", code)
	}
	if !strings.Contains(body, "is-unread") || !strings.Contains(body, "unread-badge") || !strings.Contains(body, ">Unread<") || !strings.Contains(body, "Quarterly update") {
		t.Fatalf("unread badge missing")
	}
	if !strings.Contains(body, `data-search-active="false"`) || !strings.Contains(body, `hx-trigger="search-clear"`) || !strings.Contains(body, `hx-params="account,folder"`) || !strings.Contains(body, `data-action="search#clear"`) || !strings.Contains(body, "Clear search") {
		t.Fatalf("search clear form missing")
	}
	code, body, _ = f.get("/?account="+id+"&folder=INBOX&q=hello", nil)
	if code != http.StatusOK || !strings.Contains(body, `data-search-active="true"`) || !strings.Contains(body, "Hello") {
		t.Fatalf("active search: %d", code)
	}
	code, body, _ = f.get("/?account="+id+"&folder=INBOX", map[string]string{"HX-Request": "true"})
	if code != http.StatusOK || strings.Contains(body, "<!DOCTYPE") || !strings.Contains(body, `id="mail-list"`) || !strings.Contains(body, "Quarterly update") || strings.Contains(body, "No messages match this search") {
		t.Fatalf("hx inbox fragment: %d %s", code, body)
	}
	code, body, _ = f.get("/?account="+id, map[string]string{"Accept-Language": "pt"})
	if code != http.StatusOK || !strings.Contains(body, "Não lida") || !strings.Contains(body, `placeholder="Buscar"`) || !strings.Contains(body, "Limpar busca") {
		t.Fatalf("pt unread: %d", code)
	}
	code, body, _ = f.get("/?account="+id+"&folder=INBOX&q=text:missing", nil)
	if code != http.StatusOK || !strings.Contains(body, "No messages match this search") || strings.Contains(body, "Nothing in this folder") || strings.Contains(body, "Search did not finish") {
		t.Fatalf("empty search looked like a failure or an empty folder: %d", code)
	}
	code, body, _ = f.get("/read?account="+id+"&folder=INBOX&uid=11", nil)
	if code != http.StatusOK || !strings.Contains(body, "Hello from Ada") || !strings.Contains(body, "Mark unread") {
		t.Fatalf("web read: %d", code)
	}
	code, body, _ = f.get("/read?account="+id+"&folder=INBOX&uid=10", nil)
	if code != http.StatusOK || !strings.Contains(body, "Unread note") {
		t.Fatalf("web read unread: %d", code)
	}
	if !containsUint(imap.SeenStore, 10) {
		t.Fatalf("seen store: %v", imap.SeenStore)
	}
	code, _, hdr := f.post("/unread", url.Values{"account": {id}, "folder": {"INBOX"}, "uid": {"11"}})
	if code != http.StatusSeeOther || !strings.Contains(hdr.Get("Location"), "account="+id) {
		t.Fatalf("mark unread: %d %s", code, hdr.Get("Location"))
	}
	if !containsUint(imap.UnseenStore, 11) {
		t.Fatalf("unseen store: %v", imap.UnseenStore)
	}
	code, body, _ = f.get("/?account="+id+"&folder=INBOX", nil)
	if code != http.StatusOK || !strings.Contains(body, "is-unread") || !strings.Contains(body, "Marked as unread") {
		t.Fatalf("after unread: %d", code)
	}
}

func containsUint(ids []uint32, want uint32) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func TestUnavailableSearchIsNotAnEmptyFolder(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	key, err := decodeTestKey(testSecretsKey)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			time.Sleep(30 * time.Second)
			c.Close()
		}
	}()
	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	cfg := config.Config{DataDir: t.TempDir(), SignupEnabled: true, SecretsKey: key, KuraAccountURL: "https://account.example"}
	srv := NewServer(cfg, st)
	srv.WebDir = t.TempDir()
	srv.Mail.DialTimeout = 300 * time.Millisecond
	srv.Mail.CommandTimeout = 300 * time.Millisecond
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	f := &flow{t: t, server: ts, client: client, store: st}
	_, _, _ = f.get("/login", nil)
	u := f.seedUser("you@example.com", "password1")
	_, raw, err := st.CreateToken(u.ID, "agent")
	if err != nil {
		t.Fatal(err)
	}
	payload := `{
		"display_name":"Me","from_address":"me@example.com","username":"me","password":"s3cret-mail",
		"imap_host":"` + host + `","imap_port":` + strconv.Itoa(port) + `,"imap_tls":"none",
		"smtp_host":"127.0.0.1","smtp_port":1,"smtp_tls":"none"
	}`
	code, body, out := f.apiCall(http.MethodPost, "/api/v1/accounts", raw, payload)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, body)
	}
	id := strconv.FormatFloat(out["account"].(map[string]any)["id"].(float64), 'f', 0, 64)
	f.login("you@example.com", "password1")
	code, body, _ = f.get("/?account="+id+"&q=grok", nil)
	if code != http.StatusOK || !strings.Contains(body, "Search did not finish") || strings.Contains(body, "Nothing in this folder") {
		t.Fatalf("search error page: %d has empty=%v", code, strings.Contains(body, "Nothing in this folder"))
	}
	if !strings.Contains(body, "Tansu Account") || !strings.Contains(body, "account.example") {
		t.Fatal("account link missing")
	}
	code, body, _ = f.get("/?account="+id, map[string]string{"Accept-Language": "pt"})
	if code != http.StatusOK || !strings.Contains(body, "Não foi possível carregar esta pasta") || strings.Contains(body, "Nada nesta pasta") {
		t.Fatalf("pt list error: %d", code)
	}
}

func TestAPISecretsKeyRequired(t *testing.T) {
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	u, err := st.CreateUser("you@example.com", "digest")
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := st.CreateToken(u.ID, "agent")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(config.Config{DataDir: t.TempDir()}, st)
	srv.WebDir = t.TempDir()
	ts := httptest.NewServer(srv.Routes())
	t.Cleanup(ts.Close)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/accounts", strings.NewReader(`{
		"from_address":"me@example.com","username":"me","password":"s3cret-mail",
		"imap_host":"127.0.0.1","imap_port":1,"imap_tls":"none",
		"smtp_host":"127.0.0.1","smtp_port":1,"smtp_tls":"none"
	}`))
	req.Header.Set("Authorization", "Bearer "+raw)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(string(body), "secrets_key") || strings.Contains(string(body), "s3cret-mail") {
		t.Fatalf("no key: %d %s", resp.StatusCode, body)
	}
}

func TestAPIRefusedProbeKeepsScrubbedError(t *testing.T) {
	f := newFlow(t)
	u := f.seedUser("you@example.com", "password1")
	_, raw, _ := f.store.CreateToken(u.ID, "agent")
	code, body, out := f.apiCall(http.MethodPost, "/api/v1/accounts", raw, `{
		"from_address":"me@example.com","username":"me","password":"s3cret-mail",
		"imap_host":"127.0.0.1","imap_port":1,"imap_tls":"none",
		"smtp_host":"127.0.0.1","smtp_port":1,"smtp_tls":"none"
	}`)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, body)
	}
	if strings.Contains(body, "s3cret-mail") {
		t.Fatalf("password in error: %s", body)
	}
	acct := out["account"].(map[string]any)
	if acct["last_ok"] != nil || acct["last_error"] == "" || strings.Contains(acct["last_error"].(string), "s3cret-mail") {
		t.Fatalf("probe: %+v", acct)
	}
}
