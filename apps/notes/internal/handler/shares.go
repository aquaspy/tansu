package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/aquasp/kuranotes/internal/views"
	"github.com/go-chi/chi/v5"
)

// shareURL builds the absolute share link from the request host.
func shareURL(r *http.Request, token string) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/s/%s", scheme, r.Host, token)
}

func (s *Server) handleShareCreate(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	note, err := s.Store.FindNote(user.ID, id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if !note.Shared() {
		if _, err := s.Store.GenerateShareToken(user.ID, id); err != nil {
			http.Error(w, "notes unavailable", http.StatusInternalServerError)
			return
		}
		note, _ = s.Store.FindNote(user.ID, id)
	}
	s.renderShareChange(w, r, note.ID, note.ShareToken)
}

func (s *Server) handleShareUpdate(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	token, err := s.Store.GenerateShareToken(user.ID, id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	s.renderShareChange(w, r, id, token)
}

func (s *Server) handleShareDestroy(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if _, err := s.Store.FindNote(user.ID, id); err != nil {
		s.notFound(w, r)
		return
	}
	_ = s.Store.RevokeShareToken(user.ID, id)
	s.renderShareChange(w, r, id, "")
}

func (s *Server) renderShareChange(w http.ResponseWriter, r *http.Request, id int64, token string) {
	filters := filtersValue(r.FormValue("folder"), r.FormValue("q"))
	if !isHX(r) {
		http.Redirect(w, r, views.NoteHref(id, filters), http.StatusSeeOther)
		return
	}
	p := s.page(w, r, pTitle(r, "titles.app"), "app-body")
	shareLink := ""
	if token != "" {
		shareLink = shareURL(r, token)
	}
	render(w, r, http.StatusOK, views.SharePanelResponse(p, id, token != "", shareLink, filters))
}

func (s *Server) handleSharedShow(w http.ResponseWriter, r *http.Request) {
	note, err := s.Store.FindNoteByShareToken(chi.URLParam(r, "token"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	w.Header().Set("X-Robots-Tag", "noindex, nofollow, noarchive")
	w.Header().Set("Referrer-Policy", "no-referrer")
	title := note.Title
	if title == "" {
		title = pTitle(r, "js.untitled")
	}
	p := s.page(w, r, pTitle(r, "titles.shared", "title", title), "auth-body")
	render(w, r, http.StatusOK, views.Layout(p, views.RobotsHead(), views.SharedPage(p, note)))
}
