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

// MaxTails is the full fox: nine tails for nine apps.
const MaxTails = 9

// HubData drives the account launcher.
type HubData struct {
	Email    string
	Clients  []*store.Client
	Linked   map[string]bool
	AutoLock bool
	MaxTails int
}

// LinkedCount reports the lit tails, capped at MaxTails.
func LinkedCount(d HubData) int {
	n := 0
	for _, c := range d.Clients {
		if d.Linked[c.ID] {
			n++
		}
	}
	if n > d.MaxTails {
		n = d.MaxTails
	}
	return n
}

// TailSlot is one fox tail: fan angle + lit state.
type TailSlot struct {
	Angle int
	Lit   bool
}

// TailSlots fans MaxTails tails from -80° to +80°, lighting from the
// center outward so a young fox stays symmetric.
func TailSlots(d HubData) []TailSlot {
	lit := LinkedCount(d)
	out := make([]TailSlot, 0, d.MaxTails)
	mid := (d.MaxTails - 1) / 2
	order := map[int]int{}
	for rank, dist := range []int{0, -1, 1, -2, 2, -3, 3, -4, 4} {
		if i := mid + dist; i >= 0 && i < d.MaxTails {
			order[i] = rank
		}
	}
	for i := 0; i < d.MaxTails; i++ {
		angle := -80
		if d.MaxTails > 1 {
			angle = -80 + i*160/(d.MaxTails-1)
		}
		out = append(out, TailSlot{Angle: angle, Lit: order[i] < lit})
	}
	return out
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
