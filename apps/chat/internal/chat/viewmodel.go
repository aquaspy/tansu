package chat

import (
	"github.com/aquasp/kurachat/internal/i18n"
	"github.com/aquasp/kurachat/internal/store"
	"github.com/aquasp/kurachat/internal/views"
)

func pageFor(locale i18n.Locale) views.Page {
	return views.Page{L: locale, Title: "TansuChat"}
}

// messageView builds one transcript row for pages and SSE events.
func (s *Service) messageView(locale i18n.Locale, m *store.Message, convID int64, shared bool, shareToken string) *views.MessageView {
	mv := &views.MessageView{Msg: m, Shared: shared, ShareToken: shareToken}
	if m.Role == store.RoleUser {
		if len(m.Images) == 0 {
			if imgs, err := s.Store.ListImages(m.ID); err == nil {
				m.Images = imgs
			}
		}
		if len(m.Documents) == 0 {
			if docs, err := s.Store.ListDocuments(m.ID); err == nil {
				m.Documents = docs
			}
		}
	}
	if m.Role == store.RoleAssistant {
		mv.BodyHTML = Render(orEllipsis(m.Content))
		mv.Citations = StoredCitations(m.Citations)
		if u := m.UsageMap(); u != nil {
			if model, _ := u["model"].(string); model != "" {
				mv.ModelLabel = model
			}
			_, mv.Searched = u["search_engine"]
			mv.Deep, _ = u["search_deep"].(bool)
		}
		if m.Failed() && !shared {
			if inflight, err := s.Store.InflightExists(convID); err == nil && !inflight {
				mv.CanRetry = true
			}
		}
	}
	return mv
}

// TranscriptViews builds every visible row for a conversation page.
func (s *Service) TranscriptViews(locale i18n.Locale, convID int64, shared bool, shareToken string) ([]*views.MessageView, error) {
	rows, err := s.Store.Transcript(convID)
	if err != nil {
		return nil, err
	}
	return s.TranscriptViewsFor(locale, rows, convID, shared, shareToken), nil
}

// TranscriptViewsFor builds views for explicit rows (POST responses).
func (s *Service) TranscriptViewsFor(locale i18n.Locale, rows []*store.Message, convID int64, shared bool, shareToken string) []*views.MessageView {
	out := make([]*views.MessageView, 0, len(rows))
	for _, m := range rows {
		out = append(out, s.messageView(locale, m, convID, shared, shareToken))
	}
	return out
}

// CostView builds the bar cost pill.
func (s *Service) CostView(locale i18n.Locale, convID int64) views.CostView {
	blobs, err := s.Store.TokenUsages(convID)
	if err != nil {
		return views.CostView{}
	}
	amount := FormatUSD(USDForMany(blobs))
	if amount == "" {
		return views.CostView{}
	}
	key, hint := "chat.cost_est", i18n.T(locale, "chat.cost_hint_est")
	if AllBilled(blobs) {
		key, hint = "chat.cost", i18n.T(locale, "chat.cost_hint")
	}
	return views.CostView{Text: i18n.T(locale, key, "amount", amount), Hint: hint}
}
