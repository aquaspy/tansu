package chat

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/aquasp/kurachat/internal/i18n"
	"github.com/aquasp/kurachat/internal/openrouter"
	"github.com/aquasp/kurachat/internal/store"
	"github.com/aquasp/kurachat/internal/views"
)

func (s *Service) autoTitle(conv *store.Conversation, locale i18n.Locale) {
	fresh, err := s.Store.GetConversation(conv.ID)
	if err != nil || fresh.Title != "" {
		return
	}
	first, err := s.Store.FirstUserMessage(conv.ID)
	if err != nil {
		return
	}
	title := s.requestTitle(first.Content)
	if title == "" {
		title = fallbackTitle(first.Content)
	}
	if title == "" {
		return
	}
	if err := s.Store.UpdateConversationTitle(conv.UserID, conv.ID, title); err != nil {
		return
	}
	p := pageFor(locale)
	s.Hub.Publish(conv.ID, Event{Name: views.TitleEvent(conv.ID),
		HTML: renderHTML(views.ConvTitle(conv.ID, title))})
	s.Hub.Publish(conv.ID, Event{Name: views.TitleFieldEvent(conv.ID),
		HTML: renderHTML(views.TitleField(p, conv.ID, title))})
}

func (s *Service) requestTitle(text string) string {
	client, err := s.client(s.Config.Model)
	if err != nil {
		return ""
	}
	maxOut := 24
	resp, err := client.Complete(context.Background(), []any{
		map[string]any{"role": "system", "content": "Reply with a conversation title only. Max 8 words, no quotes."},
		map[string]any{"role": "user", "content": truncateRunes(text, 400)},
	}, &maxOut, "none")
	if err != nil {
		return ""
	}
	return fallbackTitle(openrouter.MessageText(resp))
}

func fallbackTitle(text string) string {
	words := strings.Fields(strings.TrimSpace(text))
	if len(words) > 8 {
		words = words[:8]
	}
	return truncateRunes(strings.Join(words, " "), 60)
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// maybeCompact summarizes older turns once the window overflows. The full
// transcript stays in SQLite; only the model prompt shrinks.
func (s *Service) maybeCompact(conv *store.Conversation, assistant *store.Message, locale i18n.Locale) {
	rows, err := s.Store.CompactRows(conv.ID, assistant.ID, conv.SummarizedThroughID)
	if err != nil || len(rows) == 0 {
		return
	}
	type costed struct {
		m    *store.Message
		cost int
	}
	var costs []costed
	for _, m := range rows {
		if c := s.inputCostOf(m, locale); c > 0 {
			costs = append(costs, costed{m, c})
		}
	}
	prefixJSON := mustJSON(s.prefixMessages(conv, locale))
	total := tokenEstimate(prefixJSON)
	for _, c := range costs {
		total += c.cost
	}
	if total <= s.Config.WindowTokens {
		return
	}
	kept, cut := 0, len(costs)
	for i := len(costs) - 1; i >= 0; i-- {
		if kept > 0 && kept+costs[i].cost > s.Config.KeepRecentTokens {
			break
		}
		kept += costs[i].cost
		cut = i
	}
	var lines []string
	for _, c := range costs[:cut] {
		label := c.m.Content
		if label == "" {
			if imgs, _ := s.Store.ListImages(c.m.ID); len(imgs) > 0 {
				label = "[image]"
			}
		}
		if label == "" {
			continue
		}
		lines = append(lines, c.m.Role+": "+truncateRunes(label, 500))
	}
	if len(lines) == 0 {
		return
	}
	var body strings.Builder
	if prior := strings.TrimSpace(conv.Summary); prior != "" {
		body.WriteString("Previous summary:\n" + prior + "\n\n")
	}
	body.WriteString("New messages:\n" + strings.Join(lines, "\n"))
	client, err := s.client(s.Config.Model)
	if err != nil {
		return
	}
	maxOut := 180
	resp, err := client.Complete(context.Background(), []any{
		map[string]any{"role": "system", "content": "Summarize this conversation excerpt in at most 120 words. Keep facts, names, decisions, and open questions. Same language as the messages. No preamble."},
		map[string]any{"role": "user", "content": truncateRunes(body.String(), 12000)},
	}, &maxOut, "none")
	if err != nil {
		return
	}
	if text := strings.TrimSpace(openrouter.MessageText(resp)); text != "" {
		_ = s.Store.UpdateConversationSummary(conv.ID, text, costs[cut-1].m.ID)
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
