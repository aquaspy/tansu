package views

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/a-h/templ"
	"github.com/aquasp/kurahome/internal/i18n"
	"github.com/aquasp/kurahome/internal/store"
)

// Page carries the data every full page needs.
type Page struct {
	L          i18n.Locale
	Title      string
	BodyClass  string
	CSRF       string
	Notice     string
	Alert      string
	KuraLogin  bool
	AccountURL string
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

// HomeData drives the index and stack pages.
type HomeData struct {
	Profiles   []*store.Profile
	Profile    *store.Profile
	Sites      []*store.Site
	Items      []*store.StackItem
	AutoLock   bool
	OnStack    bool
	ReorderURL string
}

func (d HomeData) ProfileHref(id int64) string {
	base := "/"
	if d.OnStack {
		base = "/stack"
	}
	return base + "?profile_id=" + strconv.FormatInt(id, 10)
}

// SharePayload is the canvas-export JSON for the stack share button.
func (d HomeData) SharePayload() string {
	type row struct {
		Category string  `json:"category"`
		Choice   string  `json:"choice"`
		Origin   string  `json:"origin"`
		Note     string  `json:"note"`
		Letter   string  `json:"letter"`
		Color    string  `json:"color"`
		Icon     *string `json:"icon"`
	}
	rows := make([]row, 0, len(d.Items))
	for _, item := range d.Items {
		r := row{
			Category: item.Category,
			Choice:   item.Choice,
			Origin:   item.Origin,
			Note:     item.Note,
			Letter:   item.Letter(),
			Color:    item.Color(),
		}
		if item.IconSrc() != "" {
			icon := fmt.Sprintf("/stack_items/%d/icon", item.ID)
			r.Icon = &icon
		}
		rows = append(rows, r)
	}
	b, _ := json.Marshal(rows)
	return string(b)
}

// SharePayloadScript renders the canvas-export JSON for the stack share
// button (see I18nScript for why this lives in Go).
func (d HomeData) SharePayloadScript() templ.Component {
	return templ.Raw(`<script type="application/json" data-share-target="payload">` + d.SharePayload() + `</script>`)
}
