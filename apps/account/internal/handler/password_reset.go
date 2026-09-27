package handler

import (
	"context"
	"errors"
	"html"
	"log/slog"
	"net/http"
	"time"

	"github.com/aquasp/kuraaccount/internal/i18n"
	"github.com/aquasp/kuraaccount/internal/store"
	"github.com/aquasp/kuraaccount/internal/views"
	"golang.org/x/crypto/bcrypt"
)

func (s *Server) handlePasswordForgotNew(w http.ResponseWriter, r *http.Request) {
	if !s.Config.MailEnabled() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	s.renderForgot(w, r, http.StatusOK, "", "")
}

func (s *Server) handlePasswordForgotCreate(w http.ResponseWriter, r *http.Request) {
	if !s.Config.MailEnabled() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	l := LocaleOf(r)
	email := r.FormValue("email")
	if !s.Limiter.Allow("reset:"+clientIP(r), 10, 3*time.Minute) {
		s.renderForgot(w, r, http.StatusTooManyRequests, i18n.T(l, "auth.too_many"), email)
		return
	}
	if !store.ValidEmail(email) {
		s.renderForgot(w, r, http.StatusUnprocessableEntity, i18n.T(l, "auth.email_invalid"), email)
		return
	}
	// Count every well-formed address, known or not, so the limit
	// response does not reveal whether the inbox exists.
	norm := store.NormalizeEmail(email)
	if !s.Limiter.Allow("reset-mail:"+norm, 3, time.Hour) {
		s.renderForgot(w, r, http.StatusTooManyRequests, i18n.T(l, "auth.too_many"), email)
		return
	}
	user, err := s.Store.FindUserByEmail(email)
	if errors.Is(err, store.ErrNotFound) {
		s.renderForgotSent(w, r, norm)
		return
	}
	if err != nil {
		slog.Error("password reset lookup failed", "email", norm, "err", err)
		s.renderForgot(w, r, http.StatusUnprocessableEntity, i18n.T(l, "auth.reset_failed"), email)
		return
	}
	token, rollback, err := s.Store.SavePasswordReset(user.ID, string(l))
	if err != nil {
		slog.Error("password reset save failed", "email", norm, "err", err)
		s.renderForgot(w, r, http.StatusUnprocessableEntity, i18n.T(l, "auth.reset_failed"), email)
		return
	}
	link := s.publicURL(r, "/password/reset", token)
	// WithoutCancel so a closed browser does not look like a failed send
	// after Resend has already accepted the message.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 15*time.Second)
	defer cancel()
	sendErr := errors.New("mail not configured")
	if s.Mail != nil {
		sendErr = s.Mail.Send(ctx, norm,
			i18n.T(l, "auth.reset_subject"),
			i18n.T(l, "auth.reset_html", "url", html.EscapeString(link)),
			i18n.T(l, "auth.reset_text", "url", link))
	}
	if sendErr != nil {
		slog.Error("password reset send failed", "email", norm, "err", sendErr)
		if err := s.Store.RollbackPasswordReset(rollback); err != nil {
			slog.Error("password reset rollback failed", "email", norm, "err", err)
		}
		s.renderForgot(w, r, http.StatusBadGateway, i18n.T(l, "auth.reset_mail_failed"), email)
		return
	}
	s.renderForgotSent(w, r, norm)
}

func (s *Server) renderForgot(w http.ResponseWriter, r *http.Request, status int, alert, email string) {
	p := s.page(w, r, pTitle(r, "titles.forgot"), "auth-body")
	p.Alert = alert
	p.Next = requestNext(r)
	render(w, r, status, views.Layout(p, views.NoHead(), views.ForgotPasswordPage(p, email)))
}

func (s *Server) renderForgotSent(w http.ResponseWriter, r *http.Request, email string) {
	p := s.page(w, r, pTitle(r, "titles.forgot"), "auth-body")
	p.Next = requestNext(r)
	render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.ForgotPasswordSentPage(p, email)))
}

func (s *Server) handlePasswordResetNew(w http.ResponseWriter, r *http.Request) {
	if !s.Config.MailEnabled() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	token := r.URL.Query().Get("token")
	row, err := s.Store.FindPasswordResetByToken(token)
	if err != nil {
		s.renderResetInvalid(w, r, http.StatusBadRequest, i18n.T(LocaleOf(r), "auth.reset_invalid"), "")
		return
	}
	s.renderReset(w, r, http.StatusOK, "", token, i18n.Locale(row.Locale))
}

func (s *Server) handlePasswordResetCreate(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	if !s.Config.MailEnabled() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	token := r.FormValue("token")
	if !s.Limiter.Allow("reset-use:"+clientIP(r), 10, 3*time.Minute) {
		s.renderReset(w, r, http.StatusTooManyRequests, i18n.T(l, "auth.too_many"), token, "")
		return
	}
	row, err := s.Store.FindPasswordResetByToken(token)
	if err != nil {
		s.renderResetInvalid(w, r, http.StatusBadRequest, i18n.T(l, "auth.reset_invalid"), "")
		return
	}
	loc := i18n.Locale(row.Locale)
	password := r.FormValue("password")
	switch {
	case len(password) < 8:
		s.renderReset(w, r, http.StatusUnprocessableEntity, i18n.T(loc, "auth.password_too_short"), token, loc)
		return
	case password != r.FormValue("password_confirmation"):
		s.renderReset(w, r, http.StatusUnprocessableEntity, i18n.T(loc, "auth.password_mismatch"), token, loc)
		return
	}
	digest, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		s.renderReset(w, r, http.StatusUnprocessableEntity, i18n.T(loc, "auth.too_many"), token, loc)
		return
	}
	if err := s.Store.ConsumePasswordReset(token, string(digest)); err != nil {
		s.renderResetInvalid(w, r, http.StatusBadRequest, i18n.T(loc, "auth.reset_invalid"), loc)
		return
	}
	http.Redirect(w, r, "/login?reset=1", http.StatusSeeOther)
}

func (s *Server) renderReset(w http.ResponseWriter, r *http.Request, status int, alert, token string, l i18n.Locale) {
	p := s.page(w, r, pTitle(r, "titles.reset"), "auth-body")
	if l == i18n.EN || l == i18n.PT {
		p.L = l
		p.Title = i18n.T(l, "titles.reset")
	}
	p.Alert = alert
	render(w, r, status, views.Layout(p, views.NoHead(), views.ResetPasswordPage(p, token)))
}

func (s *Server) renderResetInvalid(w http.ResponseWriter, r *http.Request, status int, alert string, l i18n.Locale) {
	p := s.page(w, r, pTitle(r, "titles.reset"), "auth-body")
	if l == i18n.EN || l == i18n.PT {
		p.L = l
		p.Title = i18n.T(l, "titles.reset")
	}
	p.Alert = alert
	render(w, r, status, views.Layout(p, views.NoHead(), views.ResetPasswordInvalidPage(p)))
}
