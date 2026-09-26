package handler

import (
	"errors"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aquasp/kurachat/internal/docs"
	"github.com/aquasp/kurachat/internal/i18n"
	"github.com/aquasp/kurachat/internal/images"
	"github.com/aquasp/kurachat/internal/store"
	"github.com/aquasp/kurachat/internal/views"
	"github.com/go-chi/chi/v5"
)

const maxContentChars = 16384

func (s *Server) handleMessagesCreate(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	convID, err := convID(r)
	if err != nil {
		s.notFound(w, r)
		return
	}
	conv, err := s.Store.FindConversation(user.ID, convID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	deny := func(alert string) {
		if sess := SessionOf(r); sess != nil {
			_ = s.Store.SetFlash(sess.ID, "", alert)
		}
		if r.Header.Get("HX-Request") == "true" {
			w.Header().Set("HX-Redirect", "/conversations/"+itoa64(convID))
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/conversations/"+itoa64(convID), http.StatusSeeOther)
	}
	if !s.Limiter.Allow("messages:"+itoa64(user.ID), 30, 5*time.Minute) {
		deny(i18n.T(l, "chat.too_many"))
		return
	}
	if err := r.ParseMultipartForm(40 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		deny(i18n.T(l, "chat.blank"))
		return
	}
	content := r.FormValue("content")
	var files []*multipart.FileHeader
	if r.MultipartForm != nil {
		for _, fh := range r.MultipartForm.File["files[]"] {
			if fh.Size > 0 {
				files = append(files, fh)
			}
		}
	}
	if len(files) > images.MaxImages {
		deny(i18n.T(l, "chat.bad_file"))
		return
	}
	if len(content) > maxContentChars || (strings.TrimSpace(content) == "" && len(files) == 0) {
		deny(i18n.T(l, "chat.blank"))
		return
	}
	var savedImgs []*images.Saved
	var savedDocs []*docs.Saved
	purgeSaved := func() {
		for _, done := range savedImgs {
			images.Purge(s.DataDir, done.SHA256)
		}
		for _, done := range savedDocs {
			docs.Purge(s.DataDir, done.SHA256)
		}
	}
	for _, fh := range files {
		img, doc, err := saveAttachment(r.Context(), s.DataDir, fh, s.Config.LibreOfficeBin)
		if err != nil {
			purgeSaved()
			deny(i18n.T(l, "chat.bad_file"))
			return
		}
		if img != nil {
			savedImgs = append(savedImgs, img)
		} else {
			savedDocs = append(savedDocs, doc)
		}
	}
	// The picker lives outside this form and PATCHes the sticky model
	// itself, so a send only overrides it when given explicitly. The web
	// checkbox is in-form: absent means off.
	model := conv.Model
	if r.Form.Has("model") {
		model = sanitizeModel(s.Config.OpenRouterModels, r.FormValue("model"))
	}
	web := s.Config.SearchEnabled && r.FormValue("web") == "1"
	deep := s.Config.SearchEnabled && r.FormValue("deep") == "1"
	_ = s.Store.UpdateConversationSettings(user.ID, conv.ID, store.ConversationSettings{
		Model: model, Web: web, Deep: deep, Effort: conv.Effort,
		VoiceReadAloud: conv.VoiceReadAloud, VoiceAutoSend: conv.VoiceAutoSend,
	})
	userMsg, asstMsg, err := s.Store.CreateTurn(conv.ID, content, web, deep)
	if err != nil {
		purgeSaved()
		if errors.Is(err, store.ErrInflight) || store.IsUniqueViolation(err) {
			deny(i18n.T(l, "chat.in_flight"))
		} else {
			deny(i18n.T(l, "chat.blank"))
		}
		return
	}
	// Voice turns carry the STT leg's billed cost; the completer folds it
	// into the final usage blob (bogus values are ignored, never fatal).
	if cost, secs := voiceSTTForm(r); cost > 0 {
		_ = s.Store.AddUsageCost(asstMsg.ID, "stt_cost_usd", cost)
		_ = s.Store.AddUsageCost(asstMsg.ID, "stt_seconds", secs)
	}
	// Dictated turns add the polish pass the same way.
	if pc := voiceAmount(r.FormValue("polish_cost_usd"), 1); pc > 0 {
		_ = s.Store.AddUsageCost(asstMsg.ID, "polish_cost_usd", pc)
	}
	recordSaved := func() error {
		for _, sv := range savedImgs {
			if _, err := s.Store.CreateImage(userMsg.ID, sv.Filename, sv.ContentType, sv.ByteSize, sv.SHA256); err != nil {
				return err
			}
		}
		for _, sv := range savedDocs {
			if _, err := s.Store.CreateDocument(userMsg.ID, sv.Filename, sv.ContentType, sv.ByteSize, sv.SHA256); err != nil {
				return err
			}
		}
		return nil
	}
	if err := recordSaved(); err != nil {
		purgeSaved()
		_ = s.Store.DeleteMessage(asstMsg.ID)
		_ = s.Store.DeleteMessage(userMsg.ID)
		deny(i18n.T(l, "chat.bad_file"))
		return
	}
	userMsg, _ = s.Store.GetMessage(userMsg.ID)
	mvUser := s.Chat.TranscriptViewsFor(l, []*store.Message{userMsg}, conv.ID, false, "")[0]
	mvAsst := s.Chat.TranscriptViewsFor(l, []*store.Message{asstMsg}, conv.ID, false, "")[0]
	s.Chat.RunAsync(asstMsg.ID, l)

	if r.Header.Get("HX-Request") != "true" {
		http.Redirect(w, r, "/conversations/"+itoa64(conv.ID), http.StatusSeeOther)
		return
	}
	d := s.shellData(r, user.ID, conv.ID)
	p := s.page(w, r, "", "")
	render(w, r, http.StatusOK, views.MessagesCreated(p, d, mvUser, mvAsst, conv.ID))
}

// voiceSTTForm parses the hidden STT metering fields the voice controller
// attaches to voice-initiated sends. Caps keep a forged value harmless.
func voiceSTTForm(r *http.Request) (cost, seconds float64) {
	cost = voiceAmount(r.FormValue("stt_cost_usd"), 1)
	seconds = voiceAmount(r.FormValue("stt_seconds"), 600)
	if cost == 0 {
		return 0, 0
	}
	return cost, seconds
}

// voiceAmount parses one metering field, rejecting negatives and caps.
func voiceAmount(s string, cap float64) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	if f < 0 || f > cap {
		return 0
	}
	return f
}

func (s *Server) handleMessagesRetry(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	convID, err := convID(r)
	if err != nil {
		s.notFound(w, r)
		return
	}
	conv, err := s.Store.FindConversation(user.ID, convID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	mid, err := strconv.ParseInt(chi.URLParam(r, "mid"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	back := "/conversations/" + itoa64(conv.ID)
	deny := func(alert string) {
		if sess := SessionOf(r); sess != nil {
			_ = s.Store.SetFlash(sess.ID, "", alert)
		}
		hxRedirect(w, r, back)
	}
	if !s.Limiter.Allow("messages:"+itoa64(user.ID), 30, 5*time.Minute) {
		deny(i18n.T(l, "chat.too_many"))
		return
	}
	if inflight, _ := s.Store.InflightExists(conv.ID); inflight {
		deny(i18n.T(l, "chat.in_flight"))
		return
	}
	msg, err := s.Store.GetMessage(mid)
	if err != nil || msg.ConversationID != conv.ID {
		s.notFound(w, r)
		return
	}
	if !msg.Failed() {
		deny(i18n.T(l, "chat.cannot_retry"))
		return
	}
	if err := s.Store.ResetForRetry(msg.ID); err != nil {
		if store.IsUniqueViolation(err) {
			deny(i18n.T(l, "chat.in_flight"))
		} else {
			deny(i18n.T(l, "chat.cannot_retry"))
		}
		return
	}
	msg, _ = s.Store.GetMessage(msg.ID)
	s.Chat.RunAsync(msg.ID, l)
	if r.Header.Get("HX-Request") != "true" {
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	mv := s.Chat.TranscriptViewsFor(l, []*store.Message{msg}, conv.ID, false, "")[0]
	p := s.page(w, r, "", "")
	render(w, r, http.StatusOK, views.MessageArticle(p, mv, conv.ID, true))
}
