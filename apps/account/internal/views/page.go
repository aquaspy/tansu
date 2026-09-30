package views

import (
	"encoding/json"
	"net/url"
	"strconv"
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

// HubData drives the account launcher.
type HubData struct {
	Email         string
	Clients       []*store.Client
	Linked        map[string]bool
	AutoLock      bool
	Timezone      string
	TimezoneError string
}

// AppCount is how many suite apps this Account knows about.
func AppCount(d HubData) int { return len(d.Clients) }

// LinkedCount is how many of those apps this person has connected.
func LinkedCount(d HubData) int {
	n := 0
	for _, c := range d.Clients {
		if c != nil && d.Linked[c.ID] {
			n++
		}
	}
	return n
}

// Box is one rectangle in the chest mark.
type Box struct {
	X, Y, W, H int
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

// Cabinet is the chest sized to the apps this Account has.
type Cabinet struct {
	ViewBox string
	Lip     Box
	Case    Box
	FootL   Box
	FootR   Box
	Drawers []Drawer
}

// Chest builds one drawer per registered app. Connected apps light
// the faces from the lock upward.
func Chest(d HubData) Cabinet {
	const (
		rowH          = 15
		gap           = 3
		padTop        = 8
		padBot        = 12
		lipH          = 8
		caseX         = 18
		caseW         = 84
		innerX        = 24
		innerW        = 72
		footW         = 14
		footH         = 4
		emptyInterior = rowH
	)
	n := AppCount(d)
	counts := rowCounts(n)
	contentH := emptyInterior
	if len(counts) > 0 {
		contentH = len(counts)*rowH + (len(counts)-1)*gap
	}
	lipY := 8
	caseY := lipY + lipH - 2
	caseH := padTop + contentH + padBot
	footY := caseY + caseH
	c := Cabinet{
		ViewBox: "0 0 120 " + strconv.Itoa(footY+footH+4),
		Lip:     Box{X: 14, Y: lipY, W: 92, H: lipH},
		Case:    Box{X: caseX, Y: caseY, W: caseW, H: caseH},
		FootL:   Box{X: 26, Y: footY, W: footW, H: footH},
		FootR:   Box{X: 80, Y: footY, W: footW, H: footH},
	}
	y := caseY + padTop
	for r, count := range counts {
		widths := splitWidths(innerW, count, gap)
		x := innerX
		lock := r == len(counts)-1
		for i, w := range widths {
			c.Drawers = append(c.Drawers, Drawer{
				X: x, Y: y, W: w, H: rowH, Lock: lock && i == 0,
			})
			x += w + gap
		}
		y += rowH + gap
	}
	// The lock lights first, then the drawers above it.
	lit := LinkedCount(d)
	for i := 0; i < lit && i < len(c.Drawers); i++ {
		c.Drawers[len(c.Drawers)-1-i].Lit = true
	}
	return c
}

// rowCounts packs n drawers into rows. The bottom row is the lock.
// Six apps become two small, one wide, two small, and the lock.
func rowCounts(n int) []int {
	if n <= 0 {
		return nil
	}
	if n == 1 {
		return []int{1}
	}
	rest := n - 1
	wide := 2
	if rest > 8 {
		wide = 3
	}
	var rows []int
	for rest > 0 {
		if rest < wide {
			rows = append(rows, rest)
			break
		}
		if wide == 2 && rest%2 == 1 && len(rows) == 1 {
			rows = append(rows, 1)
			rest--
			continue
		}
		rows = append(rows, wide)
		rest -= wide
	}
	return append(rows, 1)
}

// splitWidths divides a row into count faces that add back to total.
func splitWidths(total, count, gap int) []int {
	if count <= 1 {
		return []int{total}
	}
	inner := total - gap*(count-1)
	w := inner / count
	extra := inner - w*count
	out := make([]int, count)
	for i := range out {
		out[i] = w
		if i < extra {
			out[i]++
		}
	}
	return out
}

// ChestLabel is the connected/total fraction, or the title when no app is registered.
func ChestLabel(p Page, d HubData) string {
	if AppCount(d) == 0 {
		return p.T("hub.title")
	}
	return p.T("hub.drawers", "count", strconv.Itoa(LinkedCount(d)), "max", strconv.Itoa(AppCount(d)))
}

// DrawerClass marks a lit face so the accent fill can follow the link.
func DrawerClass(d Drawer) string {
	if d.Lit {
		return "drawer lit"
	}
	return "drawer"
}

// ClientIcon renders the registered emoji, or the name initial.
// First-party rows do not use this; they use Mark plus AppLabel.
func ClientIcon(c *store.Client) string {
	if c == nil {
		return "◈"
	}
	if strings.TrimSpace(c.Icon) != "" {
		return c.Icon
	}
	r, _ := utf8.DecodeRuneInString(strings.TrimSpace(c.Name))
	if r == 0 {
		return "◈"
	}
	return string(r)
}

// FirstParty reports a suite app whose hub row is the wink plus a short name.
// Other clients keep the emoji from KURA_CLIENTS_JSON.
func FirstParty(c *store.Client) bool {
	if c == nil {
		return false
	}
	switch c.ID {
	case "kuranotes", "kurachat", "kuracalendar", "kuraspend", "kurapeople", "kuraemail":
		return true
	default:
		return false
	}
}

// AppLabel is the short name for a first-party app, or the registered name.
func AppLabel(p Page, c *store.Client) string {
	if c == nil {
		return ""
	}
	switch c.ID {
	case "kuranotes":
		return p.T("hub.app.notes")
	case "kurachat":
		return p.T("hub.app.assistant")
	case "kuracalendar":
		return p.T("hub.app.calendar")
	case "kuraspend":
		return p.T("hub.app.spend")
	case "kurapeople":
		return p.T("hub.app.people")
	case "kuraemail":
		return p.T("hub.app.email")
	default:
		return c.Name
	}
}

// TimezoneReady is a saved, non-empty IANA zone with no validation error.
// UTC counts: it is a real zone. Empty or rejected input still leads the hub.
func TimezoneReady(d HubData) bool {
	return d.TimezoneError == "" && strings.TrimSpace(d.Timezone) != ""
}

// IsLinked reports whether the user connected an app.
func IsLinked(d HubData, c *store.Client) bool { return d.Linked[c.ID] }

// AppHref starts the app's existing PKCE login. The Account session, when
// present, finishes authorize without another click. An empty home has
// nothing to open.
func AppHref(c *store.Client) string {
	home := strings.TrimSpace(c.Home)
	if home == "" {
		return "/"
	}
	if !strings.HasSuffix(home, "/") {
		home += "/"
	}
	href := home + "login/kura"
	// Calendar caches zoneinfo at the SSO callback. sync=1 refreshes that
	// cache when the hub session is already open. Other apps ignore the
	// query until they learn it.
	if c.ID == calendarClientID {
		return href + "?sync=1"
	}
	return href
}

// calendarClientID is the static registry id for Tansu Calendar.
const calendarClientID = "kuracalendar"

// assistantClientID is the static registry id for Tansu Assistant.
const assistantClientID = "kurachat"

// AssistantConnectHref opens Assistant on its connect-apps page after SSO.
// next is restricted to /apps by the Assistant. Empty when Assistant is
// not registered.
func AssistantConnectHref(clients []*store.Client) string {
	for _, c := range clients {
		if c == nil || c.ID != assistantClientID {
			continue
		}
		base := AppHref(c)
		if base == "/" {
			return ""
		}
		return base + "?next=" + url.QueryEscape("/apps")
	}
	return ""
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
