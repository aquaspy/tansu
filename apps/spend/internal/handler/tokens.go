package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/aquasp/kuraspend/internal/i18n"
	"github.com/aquasp/kuraspend/internal/store"
	"github.com/aquasp/kuraspend/internal/views"
	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
)

func (s *Server) handleTokensIndex(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	list, err := s.Store.ListTokens(user.ID)
	if err != nil {
		http.Error(w, "tokens unavailable", http.StatusInternalServerError)
		return
	}
	p := s.page(w, r, pTitle(r, "titles.tokens"), "auth-body")
	render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.TokensPage(p, list, "")))
}

func (s *Server) handleTokensCreate(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	list, err := s.Store.ListTokens(user.ID)
	if err != nil {
		http.Error(w, "tokens unavailable", http.StatusInternalServerError)
		return
	}
	fail := func(alert string, status int) {
		p := s.page(w, r, pTitle(r, "titles.tokens"), "auth-body")
		p.Alert = alert
		render(w, r, status, views.Layout(p, views.NoHead(), views.TokensPage(p, list, "")))
	}
	if !s.Limiter.Allow("tokens:"+itoa64(user.ID), 10, 3*time.Minute) {
		fail(i18n.T(l, "auth.too_many"), http.StatusTooManyRequests)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordDigest), []byte(r.FormValue("current_password"))) != nil {
		fail(i18n.T(l, "js.wrong_password"), http.StatusUnprocessableEntity)
		return
	}
	tok, raw, err := s.Store.CreateToken(user.ID, r.FormValue("name"))
	if err != nil {
		switch {
		case errors.Is(err, store.ErrTokenNameBlank):
			fail(i18n.T(l, "tokens.name_blank"), http.StatusUnprocessableEntity)
		case errors.Is(err, store.ErrTokenTooMany):
			fail(i18n.T(l, "tokens.too_many"), http.StatusUnprocessableEntity)
		default:
			fail(i18n.T(l, "auth.too_many"), http.StatusUnprocessableEntity)
		}
		return
	}
	_ = tok
	list, _ = s.Store.ListTokens(user.ID)
	p := s.page(w, r, pTitle(r, "titles.tokens"), "auth-body")
	render(w, r, http.StatusCreated, views.Layout(p, views.NoHead(), views.TokensPage(p, list, raw)))
}

func (s *Server) handleTokensDestroy(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Redirect(w, r, "/api_tokens/", http.StatusSeeOther)
		return
	}
	// Rails find raises RecordNotFound for another user's token.
	if _, err := s.Store.FindToken(user.ID, id); err != nil {
		s.notFound(w, r)
		return
	}
	_ = s.Store.DeleteToken(user.ID, id)
	flashNotice(s, r, i18n.T(l, "tokens.revoked"))
	http.Redirect(w, r, "/api_tokens/", http.StatusSeeOther)
}
