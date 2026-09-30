package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aquasp/kurachat/internal/chat"
	"github.com/aquasp/kurachat/internal/i18n"
	"github.com/aquasp/kurachat/internal/openrouter"
	"github.com/aquasp/kurachat/internal/store"
	"github.com/aquasp/kurachat/internal/suite"
	"github.com/aquasp/kurachat/internal/views"
)

func (s *Server) appBadges(ctx context.Context, locale i18n.Locale, userID int64) []views.AppBadge {
	if s.Chat == nil {
		return nil
	}
	rows := s.Chat.AppHealth(ctx, userID)
	var out []views.AppBadge
	for _, row := range rows {
		label := i18n.T(locale, "apps."+row.App)
		switch row.State {
		case suite.HealthOK:
			out = append(out, views.AppBadge{Label: label, State: "ok", Title: i18n.T(locale, "chat.badge_ok", "app", label)})
		case suite.HealthAuth, suite.HealthDown, suite.HealthBroken:
			title := i18n.T(locale, "chat.badge_down", "app", label)
			if row.Detail != "" {
				title += " (" + row.Detail + ")"
			}
			out = append(out, views.AppBadge{Label: label, State: "down", Title: title})
		}
	}
	return out
}

func (s *Server) handleAnonymousShow(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	d := s.shellData(r, user.ID, 0)
	d.Anonymous = true
	d.Personality = s.Store.StickyPersonality(user.ID)
	d.Anon = s.anonPage(r)
	p := s.page(w, r, pTitle(r, "titles.app"), "")
	render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.Shell(p, d)))
}

func (s *Server) anonPage(r *http.Request) *views.AnonPage {
	page := &views.AnonPage{
		ShowControls:  s.Config.ShowModelControls,
		Models:        s.Config.OpenRouterModels,
		CurrentModel:  s.Config.OpenRouterModel,
		Efforts:       nil,
		CurrentEffort: s.Config.OpenRouterReasoningEffort,
		SearchAvail:   s.Config.SearchEnabled,
	}
	if s.Config.ShowModelControls {
		page.Efforts = openrouter.Efforts
		page.ModelTiers, page.ModelPrices = s.modelTierMaps(r.Context())
	}
	return page
}

func (s *Server) handleAnonymousComplete(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	if !s.Limiter.Allow("anon:"+itoa64(user.ID), 30, 5*time.Minute) {
		http.Error(w, i18n.T(l, "chat.too_many"), http.StatusTooManyRequests)
		return
	}
	history, web, deep, model, effort, err := readAnonRequest(r)
	if err != nil {
		http.Error(w, i18n.T(l, "chat.blank"), http.StatusBadRequest)
		return
	}
	if s.Chat == nil {
		http.Error(w, i18n.T(l, "chat.failed"), http.StatusBadGateway)
		return
	}
	stream := strings.Contains(r.Header.Get("Content-Type"), "application/json") ||
		strings.Contains(r.Header.Get("Accept"), "text/event-stream")
	if !stream {
		text, err := s.Chat.StreamAnonymous(r.Context(), l, history, web, deep, model, effort, nil)
		if err != nil && strings.TrimSpace(text) == "" {
			http.Error(w, i18n.T(l, "chat.failed"), http.StatusBadGateway)
			return
		}
		d := s.shellData(r, user.ID, 0)
		d.Anonymous = true
		d.Personality = s.Store.StickyPersonality(user.ID)
		d.Anon = s.anonPage(r)
		d.Anon.SearchOn = web
		d.Anon.DeepOn = deep
		d.Anon.Messages = anonViews(history, text)
		p := s.page(w, r, pTitle(r, "titles.app"), "")
		render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.Shell(p, d)))
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	text, err := s.Chat.StreamAnonymous(r.Context(), l, history, web, deep, model, effort, func(delta string) error {
		writeAnonSSE(w, flusher, map[string]any{"delta": delta})
		return nil
	})
	if err != nil && strings.TrimSpace(text) == "" {
		writeAnonSSE(w, flusher, map[string]any{"error": i18n.T(l, "chat.failed")})
		return
	}
	writeAnonSSE(w, flusher, map[string]any{
		"done":    true,
		"content": text,
		"html":    chat.Render(text),
	})
}

func writeAnonSSE(w http.ResponseWriter, f http.Flusher, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", b)
	f.Flush()
}

func anonViews(history []chat.AnonTurn, reply string) []views.AnonMessage {
	var out []views.AnonMessage
	for _, m := range history {
		out = append(out, views.AnonMessage{Role: m.Role, Content: m.Content, HTML: chat.Render(m.Content)})
	}
	if strings.TrimSpace(reply) != "" {
		out = append(out, views.AnonMessage{Role: store.RoleAssistant, Content: reply, HTML: chat.Render(reply)})
	}
	return out
}

func readAnonRequest(r *http.Request) (history []chat.AnonTurn, web, deep bool, model, effort string, err error) {
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		raw, readErr := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if readErr != nil {
			return nil, false, false, "", "", readErr
		}
		var body struct {
			Messages []chat.AnonTurn `json:"messages"`
			Content  string          `json:"content"`
			Web      bool            `json:"web"`
			Deep     bool            `json:"deep"`
			Model    string          `json:"model"`
			Effort   string          `json:"effort"`
		}
		if json.Unmarshal(raw, &body) != nil {
			return nil, false, false, "", "", errors.New("json")
		}
		history = append(history, body.Messages...)
		if strings.TrimSpace(body.Content) != "" {
			history = append(history, chat.AnonTurn{Role: store.RoleUser, Content: body.Content})
		}
		web, deep, model, effort = body.Web, body.Deep, body.Model, body.Effort
	} else {
		if parseErr := r.ParseForm(); parseErr != nil {
			return nil, false, false, "", "", parseErr
		}
		content := r.FormValue("content")
		if strings.TrimSpace(content) == "" {
			return nil, false, false, "", "", errors.New("blank")
		}
		history = []chat.AnonTurn{{Role: store.RoleUser, Content: content}}
		web = r.FormValue("web") == "1"
		deep = r.FormValue("deep") == "1"
		model = r.FormValue("model")
		effort = r.FormValue("effort")
	}
	if len(history) == 0 || strings.TrimSpace(history[len(history)-1].Content) == "" {
		return nil, false, false, "", "", errors.New("blank")
	}
	if history[len(history)-1].Role == "" {
		history[len(history)-1].Role = store.RoleUser
	}
	return history, web, deep, model, effort, nil
}
