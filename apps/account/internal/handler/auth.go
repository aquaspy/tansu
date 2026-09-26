package handler

import (
	"context"
	"errors"
	"html"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aquasp/kuraaccount/internal/i18n"
	"github.com/aquasp/kuraaccount/internal/store"
	"github.com/aquasp/kuraaccount/internal/views"
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
	if UserOf(r) != nil && sessionUsable(r) {
		redirectNextOrHome(w, r)
		return
	}
	if !s.Config.SignupEnabled {
		http.Redirect(w, r, withNext("/login", requestNext(r)), http.StatusSeeOther)
		return
	}
	p := s.page(w, r, pTitle(r, "titles.signup"), "auth-body")
	p.Next = requestNext(r)
	render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.SignupPage(p, "")))
}

func (s *Server) handleSignupCreate(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	if !s.Config.SignupEnabled {
		if sess := SessionOf(r); sess != nil {
			_ = s.Store.SetFlash(sess.ID, "", i18n.T(l, "auth.signup_closed"))
		}
		http.Redirect(w, r, withNext("/login", requestNext(r)), http.StatusSeeOther)
		return
	}
	if !s.Limiter.Allow("signup:"+clientIP(r), 10, 3*time.Minute) {
		p := s.page(w, r, pTitle(r, "titles.signup"), "auth-body")
		p.Alert = i18n.T(l, "auth.too_many")
		p.Next = requestNext(r)
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
	if alert != "" {
		s.renderSignup(w, r, http.StatusUnprocessableEntity, alert, email)
		return
	}
	if s.Config.MailEnabled() {
		s.signupPending(w, r, email, password)
		return
	}
	s.signupImmediate(w, r, email, password)
}

func (s *Server) renderSignup(w http.ResponseWriter, r *http.Request, status int, alert, email string) {
	p := s.page(w, r, pTitle(r, "titles.signup"), "auth-body")
	p.Alert = alert
	p.Next = requestNext(r)
	render(w, r, status, views.Layout(p, views.NoHead(), views.SignupPage(p, email)))
}

func (s *Server) signupImmediate(w http.ResponseWriter, r *http.Request, email, password string) {
	l := LocaleOf(r)
	digest, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		s.renderSignup(w, r, http.StatusUnprocessableEntity, i18n.T(l, "auth.too_many"), email)
		return
	}
	user, err := s.Store.CreateUser(email, string(digest))
	if err != nil {
		alert := i18n.T(l, "auth.too_many")
		if store.IsUniqueViolation(err) {
			alert = i18n.T(l, "auth.email_taken")
		}
		s.renderSignup(w, r, http.StatusUnprocessableEntity, alert, email)
		return
	}
	sess, err := s.Store.CreateSession(user.ID)
	if err != nil {
		s.renderSignup(w, r, http.StatusUnprocessableEntity, i18n.T(l, "auth.too_many"), email)
		return
	}
	s.setSessionCookie(w, r, sess.ID)
	redirectNextOrHome(w, r)
}

// signupPending stores the password and emails a link. The users row
// does not exist until /signup/confirm accepts the token and the password.
func (s *Server) signupPending(w http.ResponseWriter, r *http.Request, email, password string) {
	l := LocaleOf(r)
	if _, err := s.Store.FindUserByEmail(email); err == nil {
		s.renderSignup(w, r, http.StatusUnprocessableEntity, i18n.T(l, "auth.email_taken"), email)
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		slog.Error("signup lookup failed", "email", store.NormalizeEmail(email), "err", err)
		s.renderSignup(w, r, http.StatusUnprocessableEntity, i18n.T(l, "auth.signup_failed"), email)
		return
	}
	if !s.Limiter.Allow("signup-mail:"+store.NormalizeEmail(email), 3, time.Hour) {
		s.renderSignup(w, r, http.StatusTooManyRequests, i18n.T(l, "auth.too_many"), email)
		return
	}
	digest, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		s.renderSignup(w, r, http.StatusUnprocessableEntity, i18n.T(l, "auth.too_many"), email)
		return
	}
	token, rollback, err := s.Store.SavePendingSignup(email, string(digest), requestNext(r), string(l))
	if err != nil {
		slog.Error("signup pending save failed", "email", store.NormalizeEmail(email), "err", err)
		s.renderSignup(w, r, http.StatusUnprocessableEntity, i18n.T(l, "auth.signup_failed"), email)
		return
	}
	confirm := s.confirmURL(r, token)
	// WithoutCancel so a closed browser does not look like a failed send
	// after Resend has already accepted the message.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 15*time.Second)
	defer cancel()
	sendErr := errors.New("mail not configured")
	if s.Mail != nil {
		sendErr = s.Mail.Send(ctx, store.NormalizeEmail(email),
			i18n.T(l, "auth.confirm_subject"),
			i18n.T(l, "auth.confirm_html", "url", html.EscapeString(confirm)),
			i18n.T(l, "auth.confirm_text", "url", confirm))
	}
	if sendErr != nil {
		slog.Error("signup confirmation send failed", "email", store.NormalizeEmail(email), "err", sendErr)
		if err := s.Store.RollbackPending(rollback); err != nil {
			slog.Error("signup confirmation rollback failed", "email", store.NormalizeEmail(email), "err", err)
		}
		s.renderSignup(w, r, http.StatusBadGateway, i18n.T(l, "auth.mail_failed"), email)
		return
	}
	p := s.page(w, r, pTitle(r, "titles.signup_sent"), "auth-body")
	p.Next = requestNext(r)
	render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.SignupSentPage(p, store.NormalizeEmail(email))))
}

func (s *Server) handleSignupConfirmNew(w http.ResponseWriter, r *http.Request) {
	if !s.Config.MailEnabled() {
		http.Redirect(w, r, "/signup", http.StatusSeeOther)
		return
	}
	token := r.URL.Query().Get("token")
	pending, err := s.Store.FindPendingByToken(token)
	if err != nil {
		s.renderConfirmInvalid(w, r, http.StatusBadRequest, i18n.T(LocaleOf(r), "auth.confirm_invalid"), "")
		return
	}
	s.renderConfirm(w, r, http.StatusOK, "", token, pending.Next, i18n.Locale(pending.Locale))
}

func (s *Server) handleSignupConfirmCreate(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	if !s.Config.MailEnabled() {
		http.Redirect(w, r, "/signup", http.StatusSeeOther)
		return
	}
	token := r.FormValue("token")
	if !s.Limiter.Allow("confirm:"+clientIP(r), 10, 3*time.Minute) {
		s.renderConfirm(w, r, http.StatusTooManyRequests, i18n.T(l, "auth.too_many"), token, "", "")
		return
	}
	pending, err := s.Store.FindPendingByToken(token)
	if err != nil {
		s.renderConfirmInvalid(w, r, http.StatusBadRequest, i18n.T(l, "auth.confirm_invalid"), "")
		return
	}
	loc := i18n.Locale(pending.Locale)
	if bcrypt.CompareHashAndPassword([]byte(pending.PasswordDigest), []byte(r.FormValue("password"))) != nil {
		burned, ferr := s.Store.RecordConfirmFailure(token)
		if ferr != nil {
			slog.Error("signup confirm attempt failed", "err", ferr)
			s.renderConfirmInvalid(w, r, http.StatusBadRequest, i18n.T(loc, "auth.confirm_invalid"), loc)
			return
		}
		if burned {
			s.renderConfirmInvalid(w, r, http.StatusBadRequest, i18n.T(loc, "auth.confirm_locked"), loc)
			return
		}
		s.renderConfirm(w, r, http.StatusUnprocessableEntity, i18n.T(loc, "js.wrong_password"), token, pending.Next, loc)
		return
	}
	user, err := s.Store.ConsumePendingSignup(token)
	if err != nil {
		if store.IsUniqueViolation(err) {
			s.renderConfirmInvalid(w, r, http.StatusUnprocessableEntity, i18n.T(loc, "auth.email_taken"), loc)
			return
		}
		s.renderConfirmInvalid(w, r, http.StatusBadRequest, i18n.T(loc, "auth.confirm_invalid"), loc)
		return
	}
	sess, err := s.Store.CreateSession(user.ID)
	if err != nil {
		s.renderConfirmInvalid(w, r, http.StatusUnprocessableEntity, i18n.T(loc, "auth.too_many"), loc)
		return
	}
	s.setSessionCookie(w, r, sess.ID)
	if pending.Next != "" {
		http.Redirect(w, r, pending.Next, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) renderConfirm(w http.ResponseWriter, r *http.Request, status int, alert, token, next string, l i18n.Locale) {
	p := s.page(w, r, pTitle(r, "titles.signup_confirm"), "auth-body")
	if l == i18n.EN || l == i18n.PT {
		p.L = l
		p.Title = i18n.T(l, "titles.signup_confirm")
	}
	p.Alert = alert
	p.Next = next
	render(w, r, status, views.Layout(p, views.NoHead(), views.SignupConfirmPage(p, token)))
}

func (s *Server) renderConfirmInvalid(w http.ResponseWriter, r *http.Request, status int, alert string, l i18n.Locale) {
	p := s.page(w, r, pTitle(r, "titles.signup_confirm"), "auth-body")
	if l == i18n.EN || l == i18n.PT {
		p.L = l
		p.Title = i18n.T(l, "titles.signup_confirm")
	}
	p.Alert = alert
	render(w, r, status, views.Layout(p, views.NoHead(), views.SignupConfirmInvalidPage(p)))
}

// confirmURL builds the link in the confirmation email. A configured
// KURA_HOST wins over the request host: behind a local proxy every
// peer is loopback, so the host allowlist does not run.
func (s *Server) confirmURL(r *http.Request, token string) string {
	host := r.Host
	if len(s.Config.KuraHosts) > 0 {
		host = s.Config.KuraHosts[0]
	}
	u := url.URL{Scheme: s.publicScheme(r), Host: host, Path: "/signup/confirm"}
	q := url.Values{}
	q.Set("token", token)
	u.RawQuery = q.Encode()
	return u.String()
}

func (s *Server) publicScheme(r *http.Request) string {
	if r.TLS != nil || s.Config.ForceSSL || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return "https"
	}
	return "http"
}

func (s *Server) handleLoginNew(w http.ResponseWriter, r *http.Request) {
	if UserOf(r) != nil {
		if sessionUsable(r) {
			redirectNextOrHome(w, r)
		} else {
			http.Redirect(w, r, withNext("/unlock", requestNext(r)), http.StatusSeeOther)
		}
		return
	}
	p := s.page(w, r, pTitle(r, "titles.login"), "auth-body")
	p.Next = requestNext(r)
	render(w, r, http.StatusOK, views.Layout(p, views.NoHead(),
		views.LoginPage(p, "", s.Config.SignupEnabled)))
}

func (s *Server) handleLoginCreate(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	email := r.FormValue("email")
	if !s.Limiter.Allow("login:"+clientIP(r), 20, 3*time.Minute) {
		p := s.page(w, r, pTitle(r, "titles.login"), "auth-body")
		p.Alert = i18n.T(l, "auth.too_many")
		p.Next = requestNext(r)
		render(w, r, http.StatusTooManyRequests, views.Layout(p, views.NoHead(),
			views.LoginPage(p, email, s.Config.SignupEnabled)))
		return
	}
	user, err := s.Store.FindUserByEmail(email)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordDigest), []byte(r.FormValue("password"))) != nil {
		p := s.page(w, r, pTitle(r, "titles.login"), "auth-body")
		p.Alert = i18n.T(l, "js.invalid_credentials")
		p.Next = requestNext(r)
		render(w, r, http.StatusUnprocessableEntity, views.Layout(p, views.NoHead(),
			views.LoginPage(p, email, s.Config.SignupEnabled)))
		return
	}
	sess, err := s.Store.CreateSession(user.ID)
	if err != nil {
		p := s.page(w, r, pTitle(r, "titles.login"), "auth-body")
		p.Alert = i18n.T(l, "auth.too_many")
		p.Next = requestNext(r)
		render(w, r, http.StatusUnprocessableEntity, views.Layout(p, views.NoHead(),
			views.LoginPage(p, email, s.Config.SignupEnabled)))
		return
	}
	s.setSessionCookie(w, r, sess.ID)
	redirectNextOrHome(w, r)
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
		http.Redirect(w, r, withNext("/login", requestNext(r)), http.StatusSeeOther)
		return
	}
	if sessionUsable(r) {
		redirectNextOrHome(w, r)
		return
	}
	p := s.page(w, r, pTitle(r, "titles.lock"), "auth-body")
	p.Next = requestNext(r)
	render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.UnlockPage(p, user.Email)))
}

func (s *Server) handleUnlockCreate(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	if user == nil {
		http.Redirect(w, r, withNext("/login", requestNext(r)), http.StatusSeeOther)
		return
	}
	if !s.Limiter.Allow("unlock:"+clientIP(r), 20, 3*time.Minute) {
		p := s.page(w, r, pTitle(r, "titles.lock"), "auth-body")
		p.Alert = i18n.T(l, "auth.too_many")
		p.Next = requestNext(r)
		render(w, r, http.StatusTooManyRequests, views.Layout(p, views.NoHead(), views.UnlockPage(p, user.Email)))
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordDigest), []byte(r.FormValue("password"))) != nil {
		p := s.page(w, r, pTitle(r, "titles.lock"), "auth-body")
		p.Alert = i18n.T(l, "js.wrong_password")
		p.Next = requestNext(r)
		render(w, r, http.StatusUnprocessableEntity, views.Layout(p, views.NoHead(), views.UnlockPage(p, user.Email)))
		return
	}
	if sess := SessionOf(r); sess != nil {
		_ = s.Store.UnlockSession(sess.ID)
	}
	s.setSuiteLock(w, r, false)
	redirectNextOrHome(w, r)
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

// safeAuthorizeNext allows only a same-host /authorize hop. Anything else
// (other paths, absolute URLs, protocol-relative URLs) is dropped.
func safeAuthorizeNext(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\r\n\\") || strings.HasPrefix(raw, "//") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || u.Host != "" || u.Path != "/authorize" {
		return ""
	}
	return u.RequestURI()
}

func requestNext(r *http.Request) string { return safeAuthorizeNext(r.FormValue("next")) }

func withNext(path, next string) string {
	if next == "" {
		return path
	}
	return path + "?next=" + url.QueryEscape(next)
}

func redirectNextOrHome(w http.ResponseWriter, r *http.Request) {
	if next := requestNext(r); next != "" {
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func itoa64(n int64) string { return strconv.FormatInt(n, 10) }
