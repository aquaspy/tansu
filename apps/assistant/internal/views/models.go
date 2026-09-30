package views

import (
	"strconv"
	"strings"
	"time"

	"github.com/aquasp/kurachat/internal/i18n"
	"github.com/aquasp/kurachat/internal/store"
)

// ConversationItem is one sidebar row.
type ConversationItem struct {
	ID        int64
	Title     string
	Preview   string
	UpdatedAt time.Time
	Active    bool
}

// RelLabel is a short relative time for a conversation row ("3d", "2 h").
func RelLabel(p Page, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	if d < time.Minute {
		if p.L == i18n.PT {
			return "agora"
		}
		return "now"
	}
	if d < time.Hour {
		n := int(d.Minutes())
		if p.L == i18n.PT {
			return strconv.Itoa(n) + " min"
		}
		return strconv.Itoa(n) + "m"
	}
	if d < 24*time.Hour {
		n := int(d.Hours())
		if p.L == i18n.PT {
			return strconv.Itoa(n) + " h"
		}
		return strconv.Itoa(n) + "h"
	}
	if d < 7*24*time.Hour {
		n := int(d.Hours() / 24)
		if p.L == i18n.PT {
			return strconv.Itoa(n) + " d"
		}
		return strconv.Itoa(n) + "d"
	}
	return i18n.TimeShort(p.L, t)
}

// CostView is nil-text when the chat has no cost yet.
type CostView struct {
	Text string // "$0.0123" or "est. $0.01"
	Hint string
}

// Citation is one validated source link.
type Citation struct {
	Title string
	URL   string
}

// MessageView is a transcript row with pre-rendered fragments.
type MessageView struct {
	Msg        *store.Message
	BodyHTML   string // sanitized markdown (assistant)
	Citations  []Citation
	ModelLabel string // assistant model from token_usage, "" when unknown
	Searched   bool   // assistant turn ran web search
	Deep       bool   // searched with the deep toggle
	CanRetry   bool   // failed && conversation not inflight
	Shared     bool   // shared page: no retry, no status polling
	ShareToken string
	Actions    []ActionCard
}

// AppRow is one sibling on the Apps page.
type AppRow struct {
	Name   string
	Label  string
	State  string // linked, off, broken, unconfigured, nokey
	Email  string
	Prefix string
}

// ActionCard is one sibling action under an assistant message.
type ActionCard struct {
	CallID       string
	Label        string
	Href         string
	Confirm      bool
	ConfirmLabel string
}

func confirmLabel(p Page, a ActionCard) string {
	if a.ConfirmLabel != "" {
		return a.ConfirmLabel
	}
	return p.T("chat.confirm_delete")
}

// ConversationDetail is the open chat in the editor column.
type ConversationDetail struct {
	Conv          *store.Conversation
	Title         string // display title
	Cost          CostView
	Messages      []*MessageView
	Inflight      bool
	ShareURL      string // "" when not shared
	Autofocus     bool   // empty transcript
	HasTitle      bool
	Models        []string          // configured slugs; picker hidden when < 2
	CurrentModel  string            // resolved sticky model
	ModelTiers    map[string]string // slug -> cheap|medium|expensive; absent when unknown
	ModelPrices   map[string]string // slug -> "$2.25/1M" blended tooltip; absent when unknown
	Efforts       []string          // effort options
	CurrentEffort string            // resolved sticky effort
	SearchOn      bool              // sticky web-search toggle (false when disabled)
	DeepOn        bool              // sticky deep-search toggle (false when disabled)
	SearchAvail   bool              // SEARCH_ENABLED: toggles hidden when false
	ShowControls  bool              // model and effort pickers
	Badges        []AppBadge        // Assistente connection status
}

// AppBadge is one linked sibling app in the Assistente status line.
type AppBadge struct {
	Label string
	State string // ok or down
	Title string
}

// AnonMessage is one browser-held anonymous turn rendered on a no-JS post.
type AnonMessage struct {
	Role    string
	Content string
	HTML    string
}

// AnonPage is the anonymous editor. Messages are not stored on the account.
type AnonPage struct {
	Messages      []AnonMessage
	ShowControls  bool
	Models        []string
	CurrentModel  string
	ModelTiers    map[string]string
	ModelPrices   map[string]string
	Efforts       []string
	CurrentEffort string
	SearchAvail   bool
	SearchOn      bool
	DeepOn        bool
}

// ShellData drives the app shell (index + show share it).
type ShellData struct {
	Query            string
	Conversations    []*ConversationItem
	HasConversations bool
	Current          *ConversationDetail // nil on bare index
	AutoLock         bool
	Anonymous        bool
	Anon             *AnonPage
	AppsNeedConnect  bool
}

// RowPreview is the sidebar second line. Empty when it repeats the title.
func RowPreview(title, raw string) string {
	raw = strings.Join(strings.Fields(raw), " ")
	if raw == "" || raw == title {
		return ""
	}
	runes := []rune(raw)
	if len(runes) > 140 {
		raw = string(runes[:140]) + "…"
	}
	return raw
}

// DisplayTitle mirrors Conversation#display_title.
func DisplayTitle(p Page, c *store.Conversation) string {
	if c.Title != "" {
		return c.Title
	}
	return p.T("chat.untitled")
}

// ModelShort is the compact menu-button label for a model slug: the part
// after the provider prefix ("x-ai/grok-4.7" -> "grok-4.7").
func ModelShort(slug string) string {
	if i := strings.LastIndex(slug, "/"); i >= 0 {
		return slug[i+1:]
	}
	return slug
}

// ModelTitle builds the picker tooltip: the full slug plus the tier and
// blended price when known ("openai/gpt-6-luna · Cheap · $0.30/1M").
func ModelTitle(p Page, d *ConversationDetail, slug string) string {
	if d == nil {
		return slug
	}
	tier := d.ModelTiers[slug]
	if tier == "" {
		return slug
	}
	s := slug + " · " + p.T("chat.tier_"+tier)
	if price := d.ModelPrices[slug]; price != "" {
		s += " · " + price
	}
	return s
}

// AriaBool renders a bool as an ARIA true/false string.
func AriaBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// BoolFlag renders a bool as the "1"/"0" forms and datasets use.
func BoolFlag(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func transcriptClass(empty bool) string {
	if empty {
		return "transcript is-empty"
	}
	return "transcript"
}

func modeSwitchClass(list bool) string {
	if list {
		return "mode-switch is-list"
	}
	return "mode-switch"
}

func heroTitle(p Page, mode string) string {
	switch mode {
	case "chat":
		return p.T("mode.hero_chat")
	case "anonymous":
		return p.T("mode.hero_anonymous")
	default:
		return p.T("mode.hero_assistant")
	}
}

func heroLede(p Page, mode string) string {
	switch mode {
	case "chat":
		return p.T("mode.chat_lede")
	case "anonymous":
		return p.T("mode.anonymous_lede")
	default:
		return p.T("mode.assistant_lede")
	}
}

// AppStatusKind is "none" (nothing linked), "ok", or "down".
func AppStatusKind(badges []AppBadge) string {
	if len(badges) == 0 {
		return "none"
	}
	for _, b := range badges {
		if b.State != "ok" {
			return "down"
		}
	}
	return "ok"
}

func downBadges(badges []AppBadge) []AppBadge {
	out := make([]AppBadge, 0)
	for _, b := range badges {
		if b.State != "ok" {
			out = append(out, b)
		}
	}
	return out
}

func joinLabels(l i18n.Locale, labels []string) string {
	n := len(labels)
	if n == 0 {
		return ""
	}
	if n == 1 {
		return labels[0]
	}
	conj := " and "
	if l == i18n.PT {
		conj = " e "
	}
	if n == 2 {
		return labels[0] + conj + labels[1]
	}
	return strings.Join(labels[:n-1], ", ") + conj + labels[n-1]
}

func appStatusOKTitle(p Page, badges []AppBadge) string {
	labels := make([]string, len(badges))
	for i, b := range badges {
		labels[i] = b.Label
	}
	tail := "connected"
	if p.L == i18n.PT {
		tail = "conectados"
	}
	return joinLabels(p.L, labels) + " " + tail
}

func appStatusDownText(p Page, badges []AppBadge) string {
	down := downBadges(badges)
	if len(down) == 1 {
		return p.T("chat.badge_down", "app", down[0].Label)
	}
	return p.T("chat.apps_down_many", "n", strconv.Itoa(len(down)))
}

func appStatusDownTitle(badges []AppBadge) string {
	down := downBadges(badges)
	parts := make([]string, len(down))
	for i, b := range down {
		parts[i] = b.Title
	}
	return strings.Join(parts, " · ")
}

func appStatusLabel(p Page, badges []AppBadge) string {
	switch AppStatusKind(badges) {
	case "none":
		return p.T("chat.apps_none")
	case "ok":
		return p.T("chat.apps_ok")
	default:
		return appStatusDownText(p, badges)
	}
}

func appStatusTitle(p Page, badges []AppBadge) string {
	switch AppStatusKind(badges) {
	case "none":
		return p.T("chat.apps_none")
	case "ok":
		return appStatusOKTitle(p, badges)
	default:
		title := appStatusDownTitle(badges)
		if title == "" {
			return appStatusLabel(p, badges)
		}
		return title
	}
}

// MsgClass builds the article class: "msg msg-user is-done" etc.
func MsgClass(m *store.Message) string {
	status := m.Status
	if status == "" {
		status = "done"
	}
	return "msg msg-" + m.Role + " is-" + status
}
