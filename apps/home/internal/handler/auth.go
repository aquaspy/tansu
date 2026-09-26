package handler

import (
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/aquasp/kurahome/internal/i18n"
	"github.com/aquasp/kurahome/internal/store"
	"github.com/aquasp/kurahome/internal/views"
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

// ensureHomeProfile mirrors User#seed_home_profile plus the
// CurrentProfile backfill: every user keeps at least one profile.
func (s *Server) ensureHomeProfile(userID int64, l i18n.Locale) error {
	if s.Store.CountProfiles(userID) > 0 {
		return nil
	}
	_, err := s.Store.CreateProfile(userID, i18n.T(l, "app.default_profile"), 0)
	return err
}

func (s *Server) handleSignupNew(w http.ResponseWriter, r *http.Request) {
	if UserOf(r) != nil && sessionUsable(r) {
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
	var errs []*store.ValError
	switch {
	case email == "":
		errs = append(errs, &store.ValError{Attr: "email", Code: "blank"})
	case !store.ValidEmail(email):
		errs = append(errs, &store.ValError{Attr: "email", Code: "invalid"})
	}
	switch {
	case password == "":
		errs = append(errs, &store.ValError{Attr: "password", Code: "blank"})
	case len(password) < 8:
		errs = append(errs, &store.ValError{Attr: "password", Code: "too_short", Count: 8})
	case password != confirm:
		errs = append(errs, &store.ValError{Attr: "password_confirmation", Code: "confirmation"})
	}
	fail := func(alert string) {
		p := s.page(w, r, pTitle(r, "titles.signup"), "auth-body")
		p.Alert = alert
		render(w, r, http.StatusUnprocessableEntity, views.Layout(p, views.NoHead(), views.SignupPage(p, email)))
	}
	if len(errs) > 0 {
		fail(fullSentence(l, "user", errs))
		return
	}
	digest, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		fail(i18n.T(l, "auth.too_many"))
		return
	}
	user, err := s.Store.CreateUser(email, string(digest))
	if err != nil {
		if store.IsUniqueViolation(err) {
			fail(i18n.T(l, "attr.user.email") + " " + i18n.T(l, "err.taken"))
		} else {
			fail(i18n.T(l, "auth.too_many"))
		}
		return
	}
	if err := s.ensureHomeProfile(user.ID, l); err != nil {
		fail(i18n.T(l, "auth.too_many"))
		return
	}
	sess, err := s.Store.CreateSession(user.ID)
	if err != nil {
		fail(i18n.T(l, "auth.too_many"))
		return
	}
	s.setSessionCookie(w, r, sess.ID)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLoginNew(w http.ResponseWriter, r *http.Request) {
	if UserOf(r) != nil {
		if sessionUsable(r) {
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
	_ = s.ensureHomeProfile(user.ID, l)
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
	if sessionUsable(r) {
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
	s.setSuiteLock(w, r, false)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLock(w http.ResponseWriter, r *http.Request) {
	if UserOf(r) == nil || SessionOf(r) == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	_ = s.Store.LockSession(SessionOf(r).ID)
	s.setSuiteLock(w, r, true)
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
		fail(fullSentence(l, "user", []*store.ValError{{Attr: "password", Code: "too_short", Count: 8}}), http.StatusUnprocessableEntity)
		return
	case password != r.FormValue("password_confirmation"):
		fail(fullSentence(l, "user", []*store.ValError{{Attr: "password_confirmation", Code: "confirmation"}}), http.StatusUnprocessableEntity)
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
