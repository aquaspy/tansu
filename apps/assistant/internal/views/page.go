package views

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"strings"

	"github.com/a-h/templ"
	"github.com/aquasp/kurachat/internal/i18n"
)

func urlQueryEscape(s string) string { return url.QueryEscape(s) }

// simpleFormat mirrors Rails simple_format: blank-line separated paragraphs,
// single newlines become <br>, content escaped.
func simpleFormat(s string) string {
	var b strings.Builder
	for _, para := range strings.Split(s, "\n\n") {
		para = strings.Trim(para, "\n")
		if strings.TrimSpace(para) == "" {
			continue
		}
		b.WriteString("<p>")
		lines := strings.Split(para, "\n")
		for i, line := range lines {
			if i > 0 {
				b.WriteString("<br>")
			}
			b.WriteString(html.EscapeString(strings.TrimRight(line, "\r")))
		}
		b.WriteString("</p>")
	}
	return b.String()
}

// Page carries the data every full page needs.
type Page struct {
	L         i18n.Locale
	Title     string
	BodyClass string
	CSRF      string
	Notice    string
	Alert     string
	KuraLogin bool
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

// Raw renders pre-sanitized HTML (markdown bodies, JSON blob).
func Raw(s string) templ.Component { return templ.Raw(s) }

// DOM ids mirror Rails dom_id exactly (prefix first: title_conversation_5).
func MsgID(id int64) string       { return fmt.Sprintf("message_%d", id) }
func MsgBodyID(id int64) string   { return fmt.Sprintf("body_message_%d", id) }
func MsgStatusID(id int64) string { return fmt.Sprintf("status_message_%d", id) }
func ConvTitleID(id int64) string { return fmt.Sprintf("title_conversation_%d", id) }
func ConvTitleFieldID(id int64) string {
	return fmt.Sprintf("title_field_conversation_%d", id)
}
func ConvCostID(id int64) string   { return fmt.Sprintf("cost_conversation_%d", id) }
func SharePanelID(id int64) string { return fmt.Sprintf("share-panel-%d", id) }
func ShareBtnID(id int64) string   { return fmt.Sprintf("share-btn-%d", id) }
