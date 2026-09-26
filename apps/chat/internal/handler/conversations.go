package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/aquasp/kurachat/internal/config"
	"github.com/aquasp/kurachat/internal/docs"
	"github.com/aquasp/kurachat/internal/i18n"
	"github.com/aquasp/kurachat/internal/images"
	"github.com/aquasp/kurachat/internal/openrouter"
	"github.com/aquasp/kurachat/internal/store"
	"github.com/aquasp/kurachat/internal/views"
	"github.com/go-chi/chi/v5"
)

func convID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}

// shellData builds the sidebar model, pruning abandoned drafts.
func (s *Server) shellData(r *http.Request, userID, currentID int64) views.ShellData {
	_ = s.Store.DeleteAbandonedDrafts(userID, currentID)
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	convs, _ := s.Store.ListConversations(userID, query)
	l := LocaleOf(r)
	d := views.ShellData{Query: query, AutoLock: AutoLockEnabled(r)}
	for _, c := range convs {
		d.Conversations = append(d.Conversations, &views.ConversationItem{
			ID: c.ID, Title: views.DisplayTitle(views.Page{L: l}, c), Active: c.ID == currentID,
		})
	}
	d.HasConversations = len(d.Conversations) > 0
	return d
}

func (s *Server) handleConversationsIndex(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	d := s.shellData(r, user.ID, 0)
	if r.Header.Get("HX-Request") == "true" {
		p := s.page(w, r, "", "")
		render(w, r, http.StatusOK, views.ConversationList(p, d))
		return
	}
	p := s.page(w, r, pTitle(r, "titles.app"), "")
	render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.Shell(p, d)))
}

func (s *Server) handleConversationsShow(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := convID(r)
	if err != nil {
		s.notFound(w, r)
		return
	}
	conv, err := s.Store.FindConversation(user.ID, id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	d := s.shellData(r, user.ID, conv.ID)
	l := LocaleOf(r)
	detail, err := s.buildDetail(l, conv)
	if err != nil {
		http.Error(w, "chat", http.StatusInternalServerError)
		return
	}
	detail.ModelTiers, detail.ModelPrices = s.modelTierMaps(r.Context())
	if conv.ShareToken != "" {
		detail.ShareURL = shareURL(r, conv.ShareToken)
	}
	d.Current = detail
	p := s.page(w, r, pTitle(r, "titles.app"), "")
	render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.Shell(p, d)))
}

func (s *Server) buildDetail(l i18n.Locale, conv *store.Conversation) (*views.ConversationDetail, error) {
	msgs, err := s.Chat.TranscriptViews(l, conv.ID, false, "")
	if err != nil {
		return nil, err
	}
	inflight, err := s.Store.InflightExists(conv.ID)
	if err != nil {
		return nil, err
	}
	return &views.ConversationDetail{
		Conv:          conv,
		Title:         views.DisplayTitle(views.Page{L: l}, conv),
		Cost:          s.Chat.CostView(l, conv.ID),
		Messages:      msgs,
		Inflight:      inflight,
		Autofocus:     len(msgs) == 0,
		HasTitle:      conv.Title != "",
		Models:        s.Config.OpenRouterModels,
		CurrentModel:  currentModel(s.Config, conv),
		Efforts:       openrouter.Efforts,
		CurrentEffort: currentEffort(s.Config, conv),
		SearchOn:      s.Config.SearchEnabled && conv.WebSearch,
		DeepOn:        s.Config.SearchEnabled && conv.DeepSearch,
		SearchAvail:   s.Config.SearchEnabled,
	}, nil
}

// modelTierMaps classifies each configured model for the picker dots:
// TierPins win, otherwise the catalog's blended $/1M price buckets via
// PriceTier. Unknown models stay absent from both maps (no dot).
func (s *Server) modelTierMaps(ctx context.Context) (tiers, prices map[string]string) {
	tiers, prices = map[string]string{}, map[string]string{}
	var catalog *openrouter.Catalog
	if s.Chat != nil {
		catalog = s.Chat.Catalog
	}
	for _, m := range s.Config.OpenRouterModels {
		if m == "" {
			continue
		}
		if pin, ok := s.Config.TierPins[m]; ok {
			tiers[m] = pin
			continue
		}
		if catalog == nil {
			continue
		}
		if p, ok := catalog.BlendedPrice(ctx, m); ok {
			tiers[m] = openrouter.PriceTier(p, s.Config.TierCheapMax, s.Config.TierExpensiveMin)
			prices[m] = "$" + strconv.FormatFloat(p, 'f', 2, 64) + "/1M"
		}
	}
	return tiers, prices
}

// sanitizeModel keeps a configured slug, else "" (server default).
func sanitizeModel(models []string, want string) string {
	want = strings.TrimSpace(want)
	for _, m := range models {
		if m != "" && m == want {
			return want
		}
	}
	return ""
}

// currentModel resolves the sticky model for display.
func currentModel(cfg config.Config, conv *store.Conversation) string {
	if sanitizeModel(cfg.OpenRouterModels, conv.Model) != "" {
		return conv.Model
	}
	return cfg.OpenRouterModel
}

// sanitizeEffort keeps a known effort, else "" (server default).
func sanitizeEffort(want string) string {
	want = strings.TrimSpace(want)
	if openrouter.ValidEffort(want) {
		return want
	}
	return ""
}

// currentEffort resolves the sticky effort for display.
func currentEffort(cfg config.Config, conv *store.Conversation) string {
	if conv.Effort != "" && openrouter.ValidEffort(conv.Effort) {
		return conv.Effort
	}
	return cfg.OpenRouterReasoningEffort
}

// handleConversationsSettings stores sticky model/search flips without a
// message send. Only provided fields change, so the picker and the toggle
// submit independently.
func (s *Server) handleConversationsSettings(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := convID(r)
	if err != nil {
		s.notFound(w, r)
		return
	}
	conv, err := s.Store.FindConversation(user.ID, id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "chat", http.StatusBadRequest)
		return
	}
	st := store.ConversationSettings{
		Model: conv.Model, Web: conv.WebSearch, Deep: conv.DeepSearch, Effort: conv.Effort,
		VoiceReadAloud: conv.VoiceReadAloud, VoiceAutoSend: conv.VoiceAutoSend,
	}
	if r.Form.Has("model") {
		st.Model = sanitizeModel(s.Config.OpenRouterModels, r.FormValue("model"))
	}
	if r.Form.Has("web_search") {
		st.Web = s.Config.SearchEnabled && r.FormValue("web_search") == "1"
	}
	if r.Form.Has("deep_search") {
		st.Deep = s.Config.SearchEnabled && r.FormValue("deep_search") == "1"
	}
	if r.Form.Has("effort") {
		st.Effort = sanitizeEffort(r.FormValue("effort"))
	}
	// Voice checkboxes always submit (a hidden 0 follows each box, and
	// FormValue takes the first), so both directions persist.
	if r.Form.Has("voice_read_aloud") {
		st.VoiceReadAloud = r.FormValue("voice_read_aloud") == "1"
	}
	if r.Form.Has("voice_auto_send") {
		st.VoiceAutoSend = r.FormValue("voice_auto_send") == "1"
	}
	if err := s.Store.UpdateConversationSettings(user.ID, conv.ID, st); err != nil {
		http.Error(w, "chat", http.StatusInternalServerError)
		return
	}
	if r.Header.Get("HX-Request") != "true" {
		http.Redirect(w, r, "/conversations/"+itoa64(conv.ID), http.StatusSeeOther)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// shareURL builds the absolute /s/ link from the request host.
func shareURL(r *http.Request, token string) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/s/" + token
}

func (s *Server) handleConversationsCreate(w http.ResponseWriter, r *http.Request) {
	draft, err := s.Store.OpenDraftFor(UserOf(r).ID)
	if err != nil {
		http.Error(w, "chat", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/conversations/"+itoa64(draft.ID), http.StatusSeeOther)
}

func (s *Server) handleConversationsUpdate(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := convID(r)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := s.Store.UpdateConversationTitle(user.ID, id, r.FormValue("conversation[title]")); err != nil {
		http.Error(w, "chat", http.StatusInternalServerError)
		return
	}
	if r.Header.Get("HX-Request") == "true" {
		d := s.shellData(r, user.ID, id)
		p := s.page(w, r, "", "")
		render(w, r, http.StatusOK, views.ConversationList(p, d))
		return
	}
	http.Redirect(w, r, "/conversations/"+itoa64(id), http.StatusSeeOther)
}

func (s *Server) handleConversationsDestroy(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := convID(r)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if _, err := s.Store.FindConversation(user.ID, id); err != nil {
		s.notFound(w, r)
		return
	}
	s.purgeConversationFiles(id)
	if err := s.Store.DeleteConversation(user.ID, id); err != nil {
		http.Error(w, "chat", http.StatusInternalServerError)
		return
	}
	s.Store.ReclaimSpace()
	if sess := SessionOf(r); sess != nil {
		_ = s.Store.SetFlash(sess.ID, i18n.T(LocaleOf(r), "chat.deleted"), "")
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleConversationsDestroyAll(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	convs, _ := s.Store.ListConversations(user.ID, "")
	for _, c := range convs {
		s.purgeConversationFiles(c.ID)
	}
	if err := s.Store.DeleteAllConversations(user.ID); err != nil {
		http.Error(w, "chat", http.StatusInternalServerError)
		return
	}
	s.Store.ReclaimSpace()
	if sess := SessionOf(r); sess != nil {
		_ = s.Store.SetFlash(sess.ID, i18n.T(LocaleOf(r), "chat.deleted_all"), "")
	}
	w.WriteHeader(http.StatusOK)
}

// purgeConversationFiles deletes uploaded bytes for every attachment.
func (s *Server) purgeConversationFiles(convID int64) {
	rows, err := s.Store.WindowRows(convID, 0, 0)
	if err != nil {
		return
	}
	for _, m := range rows {
		if imgs, err := s.Store.ListImages(m.ID); err == nil {
			for _, img := range imgs {
				images.Purge(s.DataDir, img.SHA256)
			}
		}
		if files, err := s.Store.ListDocuments(m.ID); err == nil {
			for _, doc := range files {
				docs.Purge(s.DataDir, doc.SHA256)
			}
		}
	}
}
