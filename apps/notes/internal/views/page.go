package views

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"strings"

	"github.com/a-h/templ"
	"github.com/aquasp/kuranotes/internal/i18n"
	"github.com/aquasp/kuranotes/internal/store"
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

// Raw renders pre-sanitized HTML (shared bodies, JSON blob).
func Raw(s string) templ.Component { return templ.Raw(s) }

// FolderItem is one folder row (Name "" is the inbox).
type FolderItem struct {
	Name   string
	Label  string
	Count  int
	Active bool
}

// NoteItem is one sidebar row.
type NoteItem struct {
	ID      int64
	Title   string
	Preview string
	Active  bool
}

// NoteDetail is the open note in the editor column.
type NoteDetail struct {
	Note      *store.Note
	ShareURL  string // "" when not shared
	Shared    bool
	Autofocus bool // blank draft
}

// ShellData drives the app shell (index + show share it).
type ShellData struct {
	Query    string
	Folder   string // raw filter: "all"/"inbox"/"" / name
	Folders  []*FolderItem
	Total    int
	Notes    []*NoteItem
	Current  *NoteDetail // nil on bare index
	AutoLock bool
	// Filters preserves folder+q across note links and forms.
	Filters string // e.g. "folder=work&q=deploy", "" when none
	// FolderNames feeds the folder picker (sorted, inbox excluded).
	FolderNames []string
}

// DisplayTitle mirrors note.title.presence || js.untitled.
func DisplayTitle(p Page, title string) string {
	if title != "" {
		return title
	}
	return p.T("js.untitled")
}

func SharePanelID(id int64) string { return fmt.Sprintf("share-panel-%d", id) }
func ShareBtnID(id int64) string   { return fmt.Sprintf("share-btn-%d", id) }

// NoteHref keeps the current filters on note links.
func NoteHref(id int64, filters string) string {
	if filters == "" {
		return fmt.Sprintf("/notes/%d", id)
	}
	return fmt.Sprintf("/notes/%d?%s", id, filters)
}

// NotesHref builds a /notes link with folder+q filters.
func NotesHref(folder, q string) string {
	v := url.Values{}
	if folder != "" {
		v.Set("folder", folder)
	}
	if q != "" {
		v.Set("q", q)
	}
	if len(v) == 0 {
		return "/notes/"
	}
	return "/notes/?" + v.Encode()
}
