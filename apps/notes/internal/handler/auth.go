package handler

import (
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/aquasp/kuranotes/internal/i18n"
	"github.com/aquasp/kuranotes/internal/store"
	"github.com/aquasp/kuranotes/internal/views"
	"golang.org/x/crypto/bcrypt"
)

// bcryptCost matches the Rails production work factor.
const bcryptCost = 12

func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func (s *Server) handleSignupNew(w http.ResponseWriter, r *http.Request) {
	if UserOf(r) != nil && SessionOpen(SessionOf(r), AutoLockEnabled(r)) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if !s.Config.SignupEnabled {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	p := s.page(w, r, pTitle(r, "titles.signup"), "auth-body")
	render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.SignupPage(p, "")))
}

func (s *Server) handleSignupCreate(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	if !s.Config.SignupEnabled {
		if sess := SessionOf(r); sess != nil {
			_ = s.Store.SetFlash(sess.ID, "", i18n.T(l, "auth.signup_closed"))
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if !s.Limiter.Allow("signup:"+clientIP(r), 10, 3*time.Minute) {
		p := s.page(w, r, pTitle(r, "titles.signup"), "auth-body")
		p.Alert = i18n.T(l, "auth.too_many")
		render(w, r, http.StatusTooManyRequests, views.Layout(p, views.NoHead(), views.SignupPage(p, r.FormValue("email"))))
		return
	}
	email := r.FormValue("email")
	password := r.FormValue("password")
	confirm := r.FormValue("password_confirmation")
	var alert string
	switch {
	case !store.ValidEmail(email):
		alert = i18n.T(l, "auth.email_invalid")
	case len(password) < 8:
		alert = i18n.T(l, "auth.password_too_short")
	case password != confirm:
		alert = i18n.T(l, "auth.password_mismatch")
	}
	if alert == "" {
		digest, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
		if err != nil {
			alert = i18n.T(l, "auth.too_many")
		} else if user, err := s.Store.CreateUser(email, string(digest)); err != nil {
			if store.IsUniqueViolation(err) {
				alert = i18n.T(l, "auth.email_taken")
			} else {
				alert = i18n.T(l, "auth.too_many")
			}
		} else {
			sess, err := s.Store.CreateSession(user.ID)
			if err != nil {
				alert = i18n.T(l, "auth.too_many")
			} else {
				s.setSessionCookie(w, r, sess.ID)
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
		}
	}
	p := s.page(w, r, pTitle(r, "titles.signup"), "auth-body")
	p.Alert = alert
	render(w, r, http.StatusUnprocessableEntity, views.Layout(p, views.NoHead(), views.SignupPage(p, email)))
}

func (s *Server) handleLoginNew(w http.ResponseWriter, r *http.Request) {
	if UserOf(r) != nil {
		if SessionOpen(SessionOf(r), AutoLockEnabled(r)) {
			http.Redirect(w, r, "/", http.StatusSeeOther)
		} else {
			http.Redirect(w, r, "/unlock", http.StatusSeeOther)
		}
		return
	}
	p := s.page(w, r, pTitle(r, "titles.login"), "auth-body")
	render(w, r, http.StatusOK, views.Layout(p, views.NoHead(),
		views.LoginPage(p, "", s.Config.SignupEnabled)))
}

func (s *Server) handleLoginCreate(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	email := r.FormValue("email")
	if !s.Limiter.Allow("login:"+clientIP(r), 20, 3*time.Minute) {
		p := s.page(w, r, pTitle(r, "titles.login"), "auth-body")
		p.Alert = i18n.T(l, "auth.too_many")
		render(w, r, http.StatusTooManyRequests, views.Layout(p, views.NoHead(),
			views.LoginPage(p, email, s.Config.SignupEnabled)))
		return
	}
	user, err := s.Store.FindUserByEmail(email)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordDigest), []byte(r.FormValue("password"))) != nil {
		p := s.page(w, r, pTitle(r, "titles.login"), "auth-body")
		p.Alert = i18n.T(l, "js.invalid_credentials")
		render(w, r, http.StatusUnprocessableEntity, views.Layout(p, views.NoHead(),
			views.LoginPage(p, email, s.Config.SignupEnabled)))
		return
	}
	sess, err := s.Store.CreateSession(user.ID)
	if err != nil {
		p := s.page(w, r, pTitle(r, "titles.login"), "auth-body")
		p.Alert = i18n.T(l, "auth.too_many")
		render(w, r, http.StatusUnprocessableEntity, views.Layout(p, views.NoHead(),
			views.LoginPage(p, email, s.Config.SignupEnabled)))
		return
	}
	s.setSessionCookie(w, r, sess.ID)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if sess := SessionOf(r); sess != nil {
		_ = s.Store.DeleteSession(sess.ID)
	}
	clearCookie(w, r, s.Config, SessionCookie)
	if r.Header.Get("HX-Request") == "true" {
		// app.js wipes the offline cache, then navigates.
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleUnlockNew(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if SessionOpen(SessionOf(r), AutoLockEnabled(r)) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	p := s.page(w, r, pTitle(r, "titles.lock"), "auth-body")
	render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.UnlockPage(p, user.Email)))
}

func (s *Server) handleUnlockCreate(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if !s.Limiter.Allow("unlock:"+clientIP(r), 20, 3*time.Minute) {
		p := s.page(w, r, pTitle(r, "titles.lock"), "auth-body")
		p.Alert = i18n.T(l, "auth.too_many")
		render(w, r, http.StatusTooManyRequests, views.Layout(p, views.NoHead(), views.UnlockPage(p, user.Email)))
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordDigest), []byte(r.FormValue("password"))) != nil {
		p := s.page(w, r, pTitle(r, "titles.lock"), "auth-body")
		p.Alert = i18n.T(l, "js.wrong_password")
		render(w, r, http.StatusUnprocessableEntity, views.Layout(p, views.NoHead(), views.UnlockPage(p, user.Email)))
		return
	}
	if sess := SessionOf(r); sess != nil {
		_ = s.Store.UnlockSession(sess.ID)
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLock(w http.ResponseWriter, r *http.Request) {
	if UserOf(r) == nil || SessionOf(r) == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	_ = s.Store.LockSession(SessionOf(r).ID)
	http.Redirect(w, r, "/unlock", http.StatusSeeOther)
}

func (s *Server) handlePasswordEdit(w http.ResponseWriter, r *http.Request) {
	if UserOf(r) == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	p := s.page(w, r, pTitle(r, "titles.password"), "auth-body")
	render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.PasswordPage(p)))
}

func (s *Server) handlePasswordUpdate(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	fail := func(alert string, status int) {
		p := s.page(w, r, pTitle(r, "titles.password"), "auth-body")
		p.Alert = alert
		if r.Header.Get("HX-Request") == "true" {
			render(w, r, status, views.PasswordPage(p))
			return
		}
		render(w, r, status, views.Layout(p, views.NoHead(), views.PasswordPage(p)))
	}
	if !s.Limiter.Allow("password:"+itoa64(user.ID), 10, 3*time.Minute) {
		fail(i18n.T(l, "auth.too_many"), http.StatusTooManyRequests)
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordDigest), []byte(r.FormValue("current_password"))) != nil {
		fail(i18n.T(l, "js.wrong_password"), http.StatusUnprocessableEntity)
		return
	}
	password := r.FormValue("password")
	switch {
	case len(password) < 8:
		fail(i18n.T(l, "auth.password_too_short"), http.StatusUnprocessableEntity)
		return
	case password != r.FormValue("password_confirmation"):
		fail(i18n.T(l, "auth.password_mismatch"), http.StatusUnprocessableEntity)
		return
	}
	digest, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		fail(i18n.T(l, "auth.too_many"), http.StatusUnprocessableEntity)
		return
	}
	if err := s.Store.UpdateUserPassword(user.ID, string(digest)); err != nil {
		fail(i18n.T(l, "auth.too_many"), http.StatusUnprocessableEntity)
		return
	}
	// Rotate the session like reset_session + login_as.
	if sess := SessionOf(r); sess != nil {
		_ = s.Store.DeleteSession(sess.ID)
	}
	sess, err := s.Store.CreateSession(user.ID)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	_ = s.Store.SetFlash(sess.ID, i18n.T(l, "auth.password_changed"), "")
	s.setSessionCookie(w, r, sess.ID)
	hxRedirect(w, r, "/")
}

func pTitle(r *http.Request, key string, pairs ...string) string {
	return i18n.T(LocaleOf(r), key, pairs...)
}

func itoa64(n int64) string { return strconv.FormatInt(n, 10) }
