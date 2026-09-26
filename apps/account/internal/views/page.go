package views

import (
	"encoding/json"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/a-h/templ"
	"github.com/aquasp/kuraaccount/internal/i18n"
	"github.com/aquasp/kuraaccount/internal/store"
)

// Page carries the data every full page needs.
type Page struct {
	L         i18n.Locale
	Title     string
	BodyClass string
	CSRF      string
	Notice    string
	Alert     string
	// Next is a safe /authorize return path, or empty.
	Next string
}

// T translates key with optional %{name}, value pairs.
func (p Page) T(key string, pairs ...string) string { return i18n.T(p.L, key, pairs...) }

func (p Page) Lang() string { return i18n.HTMLLang(p.L) }

// OtherLocale is the language a single click switches to.
func (p Page) OtherLocale() string {
	if p.L == i18n.PT {
		return "en"
	}
	return "pt"
}

// OtherLocaleName is the label of that switch, in the target language.
func (p Page) OtherLocaleName() string {
	if p.L == i18n.PT {
		return "English"
	}
	return "Português"
}

// I18nJSON serializes the js.* table for the #i18n script blob.
func (p Page) I18nJSON() string {
	b, _ := json.Marshal(i18n.JS(p.L))
	return string(b)
}

// Raw renders pre-sanitized HTML.
func Raw(s string) templ.Component { return templ.Raw(s) }

// I18nScript renders the js.* string table for scripts. It is built in
// Go because templ treats <script> bodies as opaque text: an @-component
// inside one would render literally. encoding/json escapes <, >, and &,
// so the blob is safe to embed raw.
func (p Page) I18nScript() templ.Component {
	return templ.Raw(`<script type="application/json" id="i18n">` + p.I18nJSON() + `</script>`)
}

// MaxDrawers is the chest: nine drawers for nine apps.
const MaxDrawers = 9

// HubData drives the account launcher.
type HubData struct {
	Email      string
	Clients    []*store.Client
	Linked     map[string]bool
	AutoLock   bool
	MaxDrawers int
}

// LinkedCount reports the lit drawers, capped at MaxDrawers.
func LinkedCount(d HubData) int {
	n := 0
	for _, c := range d.Clients {
		if d.Linked[c.ID] {
			n++
		}
	}
	if n > d.MaxDrawers {
		n = d.MaxDrawers
	}
	return n
}

// Drawer is one face of the tansu. The bottom drawer is the lock.
type Drawer struct {
	X, Y, W, H int
	Lit        bool
	Lock       bool
}

// CX is the horizontal center of the face, where the pull sits.
func (d Drawer) CX() int { return d.X + d.W/2 }

// CY is the vertical center of the face.
func (d Drawer) CY() int { return d.Y + d.H/2 }

// PullX is the left edge of a short iron pull.
func (d Drawer) PullX() int { return d.CX() - 5 }

// PullY is the top edge of that pull.
func (d Drawer) PullY() int { return d.CY() - 1 }

// Drawers is a nine-drawer tansu: two small, one wide, three small,
// two small, and a locking drawer across the bottom. Connected apps
// light it from the lock upward.
func Drawers(d HubData) []Drawer {
	lit := LinkedCount(d)
	on := map[int]bool{}
	// Bottom lock first, then the row above, then upward.
	order := []int{8, 6, 7, 4, 3, 5, 2, 0, 1}
	for i := 0; i < lit && i < len(order); i++ {
		on[order[i]] = true
	}
	faces := []Drawer{
		{X: 24, Y: 26, W: 34, H: 15},
		{X: 62, Y: 26, W: 34, H: 15},
		{X: 24, Y: 44, W: 72, H: 14},
		{X: 24, Y: 61, W: 22, H: 14},
		{X: 49, Y: 61, W: 22, H: 14},
		{X: 74, Y: 61, W: 22, H: 14},
		{X: 24, Y: 78, W: 34, H: 14},
		{X: 62, Y: 78, W: 34, H: 14},
		{X: 24, Y: 95, W: 72, H: 14, Lock: true},
	}
	for i := range faces {
		faces[i].Lit = on[i]
	}
	return faces
}

// DrawerClass marks a lit face so the accent fill can follow the link.
func DrawerClass(d Drawer) string {
	if d.Lit {
		return "drawer lit"
	}
	return "drawer"
}

// ClientIcon renders the registered emoji, or the name initial.
func ClientIcon(c *store.Client) string {
	if strings.TrimSpace(c.Icon) != "" {
		return c.Icon
	}
	r, _ := utf8.DecodeRuneInString(strings.TrimSpace(c.Name))
	if r == 0 {
		return "◈"
	}
	return string(r)
}

// IsLinked reports whether the user connected an app.
func IsLinked(d HubData, c *store.Client) bool { return d.Linked[c.ID] }

// AppHref opens a connected app at its home, and starts SSO for one
// that is not connected yet.
func AppHref(c *store.Client, linked bool) string {
	home := strings.TrimSpace(c.Home)
	if home == "" {
		return "/"
	}
	if linked {
		return home
	}
	if !strings.HasSuffix(home, "/") {
		home += "/"
	}
	return home + "login/kura"
}

// WithNext appends a next query when the authorize hop is present.
func WithNext(path, next string) string {
	if next == "" {
		return path
	}
	return path + "?next=" + url.QueryEscape(next)
}

// LockEnabledValue renders the lock controller's enabled value.
func LockEnabledValue(autoLock bool) string {
	if autoLock {
		return "true"
	}
	return "false"
}
