package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/aquasp/kurachat/internal/store"
	"github.com/aquasp/kurachat/internal/views"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handleShareCreate(w http.ResponseWriter, r *http.Request) {
	s.mutateShare(w, r, func(userID, convID int64, conv *store.Conversation) error {
		if conv.ShareToken != "" {
			return nil
		}
		_, err := s.Store.GenerateShareToken(userID, convID)
		return err
	})
}

func (s *Server) handleShareUpdate(w http.ResponseWriter, r *http.Request) {
	s.mutateShare(w, r, func(userID, convID int64, conv *store.Conversation) error {
		_, err := s.Store.GenerateShareToken(userID, convID)
		return err
	})
}

func (s *Server) handleShareDestroy(w http.ResponseWriter, r *http.Request) {
	s.mutateShare(w, r, func(userID, convID int64, conv *store.Conversation) error {
		return s.Store.RevokeShareToken(userID, convID)
	})
}

func (s *Server) mutateShare(w http.ResponseWriter, r *http.Request, mutate func(userID, convID int64, conv *store.Conversation) error) {
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
	if err := mutate(user.ID, conv.ID, conv); err != nil {
		http.Error(w, "chat", http.StatusInternalServerError)
		return
	}
	conv, _ = s.Store.FindConversation(user.ID, conv.ID)
	back := "/conversations/" + itoa64(conv.ID)
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		back += "?q=" + q
	}
	if r.Header.Get("HX-Request") != "true" {
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	p := s.page(w, r, "", "")
	url := ""
	if conv.ShareToken != "" {
		url = shareURL(r, conv.ShareToken)
	}
	render(w, r, http.StatusOK, views.SharePanelResponse(p, conv.ID, conv.ShareToken != "", url,
		strings.TrimSpace(r.URL.Query().Get("q"))))
}

func (s *Server) handleSharedShow(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	conv, err := s.Store.FindConversationByShareToken(token)
	if err != nil {
		s.notFound(w, r)
		return
	}
	l := LocaleOf(r)
	msgs, err := s.Chat.TranscriptViews(l, conv.ID, true, token)
	if err != nil {
		http.Error(w, "chat", http.StatusInternalServerError)
		return
	}
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
	w.Header().Set("Referrer-Policy", "no-referrer")
	p := s.page(w, r, pTitle(r, "titles.shared", "title", views.DisplayTitle(views.Page{L: l}, conv)), "auth-body")
	render(w, r, http.StatusOK, views.Layout(p, views.RobotsHead(),
		views.SharedChatPage(p, views.DisplayTitle(views.Page{L: l}, conv), msgs)))
}

func (s *Server) handleSharedImage(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	conv, err := s.Store.FindConversationByShareToken(token)
	if err != nil {
		s.notFound(w, r)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	img, err := s.Store.GetImage(id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	msg, err := s.Store.GetMessage(img.MessageID)
	if err != nil || msg.ConversationID != conv.ID {
		s.notFound(w, r)
		return
	}
	s.serveImage(w, r, img)
}

func (s *Server) handleSharedDocument(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	conv, err := s.Store.FindConversationByShareToken(token)
	if err != nil {
		s.notFound(w, r)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	doc, err := s.Store.GetDocument(id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	msg, err := s.Store.GetMessage(doc.MessageID)
	if err != nil || msg.ConversationID != conv.ID {
		s.notFound(w, r)
		return
	}
	s.serveDocument(w, r, doc)
}
