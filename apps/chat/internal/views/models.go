package views

import (
	"strings"

	"github.com/aquasp/kurachat/internal/store"
)

// ConversationItem is one sidebar row.
type ConversationItem struct {
	ID     int64
	Title  string
	Active bool
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
}

// ShellData drives the app shell (index + show share it).
type ShellData struct {
	Query            string
	Conversations    []*ConversationItem
	HasConversations bool
	Current          *ConversationDetail // nil on bare index
	AutoLock         bool
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

// MsgClass builds the article class: "msg msg-user is-done" etc.
func MsgClass(m *store.Message) string {
	status := m.Status
	if status == "" {
		status = "done"
	}
	return "msg msg-" + m.Role + " is-" + status
}
