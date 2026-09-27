package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The person form is one server-rendered <dialog>. It must not carry
// the open attribute on first paint (list or empty state), on the
// index or on a person's detail page.
func TestPersonDialogMarkupStartsClosed(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	f.login(u.Email, "secret-password")

	code, body, _ := f.get("/", nil)
	if code != http.StatusOK {
		t.Fatalf("index: %d", code)
	}
	assertClosedPersonDialog(t, body)
	mustContain(t, body, "Nobody here yet")
	mustContain(t, body, `class="fab"`)

	code, _, h := f.post("/people", personForm("Ada Lovelace"), nil)
	if code != http.StatusSeeOther {
		t.Fatalf("create: %d", code)
	}
	code, body, _ = f.get("/", nil)
	if code != http.StatusOK {
		t.Fatalf("index with people: %d", code)
	}
	assertClosedPersonDialog(t, body)
	mustContain(t, body, "Ada Lovelace")
	if strings.Contains(body, "Nobody here yet") {
		t.Fatal("populated index still shows the empty state")
	}

	code, body, _ = f.get(h.Get("Location"), nil)
	if code != http.StatusOK {
		t.Fatalf("show: %d", code)
	}
	assertClosedPersonDialog(t, body)
}

func assertClosedPersonDialog(t *testing.T, body string) {
	t.Helper()
	const marker = `<dialog class="confirm-dialog form-dialog person-dialog"`
	if strings.Count(body, marker) != 1 {
		t.Fatalf("person dialog count = %d, want 1", strings.Count(body, marker))
	}
	i := strings.Index(body, marker)
	j := strings.Index(body[i:], ">")
	if j < 0 {
		t.Fatal("person dialog tag not closed")
	}
	tag := body[i : i+j]
	if strings.Contains(tag, " open") || strings.Contains(tag, "\topen") {
		t.Fatalf("person dialog is open in markup: %s", tag)
	}
}

// Locks the CSS/JS contract the browser test exercises: closed dialogs
// stay display:none, + is hidden while open, and a second open is ignored.
func TestPersonDialogOpenContract(t *testing.T) {
	css := string(mustRead(t, filepath.Join("..", "..", "web", "static", "css", "input.css")))
	js := string(mustRead(t, filepath.Join("..", "..", "web", "static", "js", "app.js")))

	rules := parseCSSRules(css)
	var closedNone, openFlex, hideAdd, fieldsScroll, actionsStick, safePad, safeMargin bool
	for _, rule := range rules {
		sel, body := rule[0], rule[1]
		if strings.Contains(cssSubject(sel), "person-dialog") && strings.Contains(body, "display:") && !strings.Contains(sel, "[open]") {
			t.Errorf("display on person dialog is not gated on [open]: %s {%s}", sel, strings.TrimSpace(body))
		}
		if sel == ".confirm-dialog.person-dialog:not([open])" && strings.Contains(body, "display: none") {
			closedNone = true
		}
		if sel == ".confirm-dialog.person-dialog[open]" && strings.Contains(body, "display: flex") {
			openFlex = true
			if strings.Contains(body, "env(safe-area-inset-bottom)") && strings.Contains(body, "padding-bottom") {
				safePad = true
			}
		}
		if strings.Contains(sel, ":has(dialog.person-dialog[open])") &&
			strings.Contains(sel, `composer#openPerson`) &&
			strings.Contains(body, "display: none") {
			hideAdd = true
		}
		if sel == ".person-fields" && strings.Contains(body, "overflow: auto") {
			fieldsScroll = true
		}
		if sel == ".person-actions" && strings.Contains(body, "flex: none") {
			actionsStick = true
		}
		if sel == ".confirm-dialog.person-dialog[open]" && strings.Contains(body, "margin-top") &&
			strings.Contains(body, "env(safe-area-inset-top)") {
			safeMargin = true
		}
	}
	if !closedNone {
		t.Error("missing .confirm-dialog.person-dialog:not([open]) { display: none }")
	}
	if !openFlex {
		t.Error("open person dialog lost display:flex")
	}
	if !hideAdd {
		t.Error("missing rule hiding composer#openPerson while the person dialog is open")
	}
	if !fieldsScroll || !actionsStick || !safePad || !safeMargin {
		t.Errorf("mobile save layout regressed: fields=%v actions=%v pad=%v margin=%v", fieldsScroll, actionsStick, safePad, safeMargin)
	}

	start := strings.Index(js, "openPerson({ event, scope })")
	if start < 0 {
		t.Fatal("openPerson missing")
	}
	fn := js[start:]
	guard := strings.Index(fn, "if (!box || box.open) return;")
	show := strings.Index(fn, "box.showModal()")
	if guard < 0 || show < 0 || guard > show {
		t.Fatal("openPerson must return before showModal when the dialog is already open")
	}
}

func TestPersonDialogBrowser(t *testing.T) {
	chrome := findChrome()
	if chrome == "" {
		t.Skip("chrome not installed")
	}
	dir := t.TempDir()
	css := string(mustRead(t, filepath.Join("..", "..", "web", "static", "css", "input.css")))
	css = strings.TrimPrefix(css, `@import "tailwindcss";`)
	if err := os.WriteFile(filepath.Join(dir, "app.css"), []byte(css), 0o644); err != nil {
		t.Fatal(err)
	}
	js := mustRead(t, filepath.Join("..", "..", "web", "static", "js", "app.js"))
	if err := os.WriteFile(filepath.Join(dir, "app.js"), js, 0o644); err != nil {
		t.Fatal(err)
	}
	html := `<!doctype html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="stylesheet" href="app.css"></head>
<body>
<div class="people" data-controller="composer">
  <header class="people-top">
    <button type="button" class="icon-btn" data-action="composer#openPerson" aria-label="Add person">+</button>
  </header>
  <main class="people-inner">
    <section class="empty"><h1>Nobody here yet</h1></section>
  </main>
  <button type="button" class="fab" data-action="composer#openPerson" aria-label="Add person">+</button>
  <dialog class="confirm-dialog form-dialog person-dialog" data-composer-target="personBox">
    <p class="dialog-heading">New person</p>
    <form class="auth-form person-form" action="/people" method="post">
      <div class="person-fields">
        <label>Name <input name="name" required></label>
        <label>Notes <textarea name="notes" rows="8">line</textarea></label>
        <label>Favorites <textarea name="favorites" rows="8">line</textarea></label>
        <label>Address <input name="address"></label>
        <label>Phone <input name="phone"></label>
        <label>Email <input name="email"></label>
      </div>
      <div class="person-actions">
        <button type="button" class="btn-dialog" data-action="composer#closePerson">Cancel</button>
        <button type="submit" class="btn-dialog primary">Save</button>
      </div>
    </form>
  </dialog>
</div>
<script src="app.js"></script>
<script>
const dlg = document.querySelector("dialog.person-dialog");
const fab = document.querySelector(".fab");
const headerAdd = document.querySelector(".icon-btn");
const empty = document.querySelector(".empty");
const fields = document.querySelector(".person-fields");
const save = document.querySelector(".person-actions .btn-dialog.primary");
function box(el) {
  const r = el.getBoundingClientRect();
  return { w: r.width, h: r.height, top: r.top, bottom: r.bottom };
}
const out = {
  dialogs: document.querySelectorAll("dialog.person-dialog").length,
  closedOpen: dlg.open,
  closedDisplay: getComputedStyle(dlg).display,
  closedBox: box(dlg),
  listH: empty.getBoundingClientRect().height,
  fabClosed: getComputedStyle(fab).display
};
fab.click();
const saveBox = box(save);
out.afterOpen = dlg.open;
out.afterDisplay = getComputedStyle(dlg).display;
out.dialogsAfter = document.querySelectorAll("dialog").length;
out.openPersonDialogs = document.querySelectorAll("dialog.person-dialog[open]").length;
out.fabOpen = getComputedStyle(fab).display;
out.headerOpen = getComputedStyle(headerAdd).display;
out.fieldsOverflow = getComputedStyle(fields).overflowY;
out.fieldsScrollable = fields.scrollHeight > fields.clientHeight + 1;
fields.scrollTop = 80;
out.fieldsScrolled = fields.scrollTop > 0;
out.saveBox = saveBox;
out.vh = window.innerHeight;
out.saveOnScreen = saveBox.top >= 0 && saveBox.bottom <= window.innerHeight + 1 && saveBox.h > 0;
const before = document.querySelectorAll("dialog").length;
fab.click();
headerAdd.click();
document.querySelector(".empty")?.querySelector("button")?.click();
out.dialogsAfterSecond = document.querySelectorAll("dialog").length;
out.stillOne = out.dialogsAfterSecond === before && document.querySelectorAll("dialog.person-dialog[open]").length === 1;
dlg.close();
out.fabAfterClose = getComputedStyle(fab).display;
out.headerAfterClose = getComputedStyle(headerAdd).display;
out.closedAgain = !dlg.open && getComputedStyle(dlg).display === "none";
document.documentElement.dataset.result = JSON.stringify(out);
</script>
</body></html>`
	page := filepath.Join(dir, "index.html")
	if err := os.WriteFile(page, []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, chrome,
		"--headless", "--disable-gpu", "--no-sandbox", "--disable-dev-shm-usage",
		"--user-data-dir="+filepath.Join(dir, "profile"),
		"--window-size=360,480",
		"--timeout=4000",
		"--dump-dom", page,
	)
	raw, err := cmd.Output()
	if len(raw) == 0 {
		t.Fatalf("chrome produced no dom: %v", err)
	}
	const key = `data-result="`
	i := strings.Index(string(raw), key)
	if i < 0 {
		t.Fatalf("browser result missing: %s", tail(string(raw), 500))
	}
	rest := string(raw)[i+len(key):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		t.Fatal("browser result not terminated")
	}
	payload, err := jsonUnquoteHTML(rest[:j])
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("result json: %v\n%s", err, payload)
	}
	t.Logf("browser %s", payload)

	eq := func(k, want string) {
		t.Helper()
		if got[k] != want {
			t.Errorf("%s = %#v, want %q", k, got[k], want)
		}
	}
	truthy := func(k string) {
		t.Helper()
		v, ok := got[k].(bool)
		if !ok || !v {
			t.Errorf("%s = %#v, want true", k, got[k])
		}
	}
	falsy := func(k string) {
		t.Helper()
		v, ok := got[k].(bool)
		if !ok || v {
			t.Errorf("%s = %#v, want false", k, got[k])
		}
	}
	if got["dialogs"] != float64(1) {
		t.Errorf("dialogs = %#v, want 1", got["dialogs"])
	}
	falsy("closedOpen")
	eq("closedDisplay", "none")
	if h, _ := got["listH"].(float64); h < 1 {
		t.Errorf("empty list not visible on load: %#v", got["listH"])
	}
	eq("fabClosed", "grid")
	truthy("afterOpen")
	eq("afterDisplay", "flex")
	if got["dialogsAfter"] != float64(1) || got["openPersonDialogs"] != float64(1) {
		t.Errorf("open created extra dialogs: after=%v open=%v", got["dialogsAfter"], got["openPersonDialogs"])
	}
	eq("fabOpen", "none")
	eq("headerOpen", "none")
	if ov, _ := got["fieldsOverflow"].(string); ov != "auto" && ov != "scroll" {
		t.Errorf("fields overflow = %#v, want auto", got["fieldsOverflow"])
	}
	truthy("fieldsScrollable")
	truthy("fieldsScrolled")
	truthy("saveOnScreen")
	truthy("stillOne")
	eq("fabAfterClose", "grid")
	eq("headerAfterClose", "grid")
	truthy("closedAgain")
}

func jsonUnquoteHTML(s string) (string, error) {
	// dump-dom escapes quotes in attributes as &quot;.
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&#34;", `"`)
	s = strings.ReplaceAll(s, "&amp;", "&")
	return s, nil
}

func findChrome() string {
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// cssSubject is the last compound of sel, so descendant rules such as
// ".person-dialog .person-form" are not treated as styling the dialog.
func cssSubject(sel string) string {
	depth := 0
	last := 0
	for i := 0; i < len(sel); i++ {
		switch sel[i] {
		case '(', '[':
			depth++
		case ')', ']':
			if depth > 0 {
				depth--
			}
		case ' ', '>', '+', '~':
			if depth == 0 {
				last = i + 1
			}
		}
	}
	return strings.TrimSpace(sel[last:])
}

// parseCSSRules returns selector/body pairs, descending into @media and @supports.
func parseCSSRules(css string) [][2]string {
	css = stripCSSComments(css)
	var rules [][2]string
	var walk func(string)
	walk = func(s string) {
		i := 0
		for i < len(s) {
			for i < len(s) && isCSSSpace(s[i]) {
				i++
			}
			if i >= len(s) {
				return
			}
			start := i
			for i < len(s) && s[i] != '{' {
				i++
			}
			if i >= len(s) {
				return
			}
			prelude := strings.TrimSpace(s[start:i])
			i++
			bodyStart := i
			depth := 1
			for i < len(s) && depth > 0 {
				switch s[i] {
				case '{':
					depth++
				case '}':
					depth--
				}
				i++
			}
			if depth != 0 {
				return
			}
			body := s[bodyStart : i-1]
			if strings.HasPrefix(prelude, "@media") || strings.HasPrefix(prelude, "@supports") {
				walk(body)
				continue
			}
			if prelude == "" || strings.HasPrefix(prelude, "@") {
				continue
			}
			rules = append(rules, [2]string{prelude, body})
		}
	}
	walk(css)
	return rules
}

func stripCSSComments(css string) string {
	var b strings.Builder
	for i := 0; i < len(css); {
		if i+1 < len(css) && css[i] == '/' && css[i+1] == '*' {
			end := strings.Index(css[i+2:], "*/")
			if end < 0 {
				break
			}
			i += 2 + end + 2
			continue
		}
		b.WriteByte(css[i])
		i++
	}
	return b.String()
}

func isCSSSpace(b byte) bool {
	return b == ' ' || b == '\n' || b == '\t' || b == '\r'
}
