// Package handler wires routes, middleware, and page handlers.
package handler

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aquasp/kuranotes/internal/config"
	"github.com/aquasp/kuranotes/internal/i18n"
	"github.com/aquasp/kuranotes/internal/store"
	"github.com/go-chi/chi/v5"
)

const (
	SessionCookie  = "kura_session"
	CSRFCookie     = "kura_csrf"
	AutoLockCookie = "kura_auto_lock"

	// IdleAfter mirrors Locking::IDLE_AFTER (absolute, not sliding).
	IdleAfter = 15 * time.Minute
)

type Server struct {
	Config  config.Config
	Store   *store.Store
	Limiter *RateLimiter
	DataDir string
	WebDir  string
}

func NewServer(cfg config.Config, st *store.Store) *Server {
	return &Server{Config: cfg, Store: st, Limiter: NewRateLimiter(),
		DataDir: cfg.DataDir, WebDir: "web/static"}
}

type ctxKey string

const (
	localeKey  ctxKey = "locale"
	sessionKey ctxKey = "session"
	userKey    ctxKey = "user"
	flashKey   ctxKey = "flash"
)

// Flash carries one-shot notice/alert into templates.
type Flash struct {
	Notice string
	Alert  string
}

func LocaleOf(r *http.Request) i18n.Locale {
	if l, ok := r.Context().Value(localeKey).(i18n.Locale); ok {
		return l
	}
	return i18n.EN
}

func SessionOf(r *http.Request) *store.Session {
	s, _ := r.Context().Value(sessionKey).(*store.Session)
	return s
}

func UserOf(r *http.Request) *store.User {
	u, _ := r.Context().Value(userKey).(*store.User)
	return u
}

func FlashOf(r *http.Request) Flash {
	if f, ok := r.Context().Value(flashKey).(Flash); ok {
		return f
	}
	return Flash{}
}

func (s *Server) localeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l := i18n.FromCookie(r.Header.Get("Accept-Language"), localeCookieValue(r))
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), localeKey, l)))
	})
}

// sessionMiddleware loads the session + user and consumes the flash.
// Missing/invalid sessions are anonymous, not errors.
func (s *Server) sessionMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if c, err := r.Cookie(SessionCookie); err == nil && c.Value != "" {
			if sess, err := s.Store.TouchSession(c.Value); err == nil {
				// Renew the persistent cookie alongside last_seen_at so the
				// login survives browser restarts (sliding window).
				s.setSessionCookie(w, r, sess.ID)
				ctx = context.WithValue(ctx, sessionKey, sess)
				if u, err := s.Store.FindUser(sess.UserID); err == nil {
					ctx = context.WithValue(ctx, userKey, u)
				}
				if sess.FlashNotice != "" || sess.FlashAlert != "" {
					notice, alert, _ := s.Store.TakeFlash(sess.ID)
					ctx = context.WithValue(ctx, flashKey, Flash{Notice: notice, Alert: alert})
				}
			} else {
				clearCookie(w, r, s.Config, SessionCookie)
			}
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserOf(r) == nil || SessionOf(r) == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// SessionOpen mirrors Locking.session_open?.
func SessionOpen(sess *store.Session, autoLock bool) bool {
	if sess == nil || sess.UnlockedAt == nil {
		return false
	}
	if !autoLock {
		return true
	}
	return sess.UnlockedAt.After(time.Now().Add(-IdleAfter))
}

func AutoLockEnabled(r *http.Request) bool {
	c, err := r.Cookie(AutoLockCookie)
	return err == nil && c.Value == "1"
}

const suiteLockCookie = "kura_suite_lock"

// SuiteLocked is the shared lock for every app on this host (or parent
// domain). Locking one app sets it; unlocking clears it.
func SuiteLocked(r *http.Request) bool {
	c, err := r.Cookie(suiteLockCookie)
	return err == nil && c.Value == "1"
}

func sessionUsable(r *http.Request) bool {
	if SuiteLocked(r) {
		return false
	}
	return SessionOpen(SessionOf(r), AutoLockEnabled(r))
}

// sharedCookieDomain is the parent of KURA_HOST so notes.example.com and
// calendar.example.com see the same cookie. Localhost and IP addresses
// stay host-only, which already covers every port on that host.
func sharedCookieDomain(hosts []string) string {
	if len(hosts) == 0 {
		return ""
	}
	host := hosts[0]
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimPrefix(strings.ToLower(host), ".")
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") || net.ParseIP(host) != nil {
		return ""
	}
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return ""
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

func (s *Server) setSuiteLock(w http.ResponseWriter, r *http.Request, locked bool) {
	value := ""
	maxAge := -1
	if locked {
		value = "1"
		maxAge = 365 * 24 * 3600
	}
	http.SetCookie(w, &http.Cookie{
		Name:     suiteLockCookie,
		Value:    value,
		Path:     "/",
		Domain:   sharedCookieDomain(s.Config.KuraHosts),
		MaxAge:   maxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.secureCookies(r),
	})
}

func (s *Server) requireUnlock(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserOf(r) != nil && !sessionUsable(r) {
			http.Redirect(w, r, "/unlock", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// csrfMiddleware uses the double-submit cookie pattern: safe methods ensure
// the cookie exists; mutating methods must echo it back via form field or
// the X-CSRF-Token header (set globally for htmx in app.js). The Bearer
// token API is exempt: it never reads cookies, like ActionController::API.
func (s *Server) csrfMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			s.ensureCSRFCookie(w, r)
			next.ServeHTTP(w, r)
		default:
			c, err := r.Cookie(CSRFCookie)
			if err != nil || c.Value == "" {
				http.Error(w, "bad csrf cookie", http.StatusForbidden)
				return
			}
			sent := r.Header.Get("X-CSRF-Token")
			if sent == "" {
				sent = r.FormValue("csrf_token")
			}
			if sent == "" || subtle.ConstantTimeCompare([]byte(sent), []byte(c.Value)) != 1 {
				http.Error(w, "bad csrf token", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		}
	})
}

// CSRFToken returns the cookie value for templates to embed in forms.
func (s *Server) CSRFToken(w http.ResponseWriter, r *http.Request) string {
	return s.ensureCSRFCookie(w, r)
}

func (s *Server) ensureCSRFCookie(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(CSRFCookie); err == nil && c.Value != "" {
		return c.Value
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	token := hex.EncodeToString(buf)
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookie,
		Value:    token,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
		Secure:   s.secureCookies(r),
	})
	return token
}

func (s *Server) secureCookies(r *http.Request) bool {
	if s.Config.ForceSSL {
		return true
	}
	return r.TLS != nil
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, id string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.secureCookies(r),
		MaxAge:   int(store.SessionMaxAge.Seconds()),
		Expires:  time.Now().Add(store.SessionMaxAge),
	})
}

func clearCookie(w http.ResponseWriter, r *http.Request, cfg config.Config, name string) {
	secure := cfg.ForceSSL || r.TLS != nil
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/",
		MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: secure})
}

// hostMiddleware enforces KURA_HOST like Rails config.hosts. Loopback is
// always allowed (dev + container healthcheck).
func (s *Server) hostMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(s.Config.KuraHosts) > 0 && !isLoopback(r) {
			host := r.Host
			if h, _, err := net.SplitHostPort(host); err == nil {
				host = h
			}
			ok := false
			for _, h := range s.Config.KuraHosts {
				if strings.EqualFold(h, host) {
					ok = true
					break
				}
			}
			if !ok {
				http.Error(w, "blocked host", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func isLoopback(r *http.Request) bool {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// sslMiddleware redirects plain HTTP to HTTPS when FORCE_SSL is set,
// trusting X-Forwarded-Proto from the proxy. Loopback is exempt.
func (s *Server) sslMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Config.ForceSSL && r.TLS == nil && !isLoopback(r) &&
			!strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			u := *r.URL
			u.Scheme = "https"
			u.Host = r.Host
			http.Redirect(w, r, u.String(), http.StatusPermanentRedirect)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RateLimiter is a fixed-window in-memory limiter (single process).
type RateLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func NewRateLimiter() *RateLimiter { return &RateLimiter{hits: map[string][]time.Time{}} }

// Allow reports whether key may act (limit actions per window).
func (l *RateLimiter) Allow(key string, limit int, window time.Duration) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	cut := now.Add(-window)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= limit {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	// Opportunistic sweep so idle keys don't accumulate.
	if len(l.hits)%64 == 0 {
		for k, v := range l.hits {
			alive := v[:0]
			for _, t := range v {
				if t.After(cut) {
					alive = append(alive, t)
				}
			}
			if len(alive) == 0 {
				delete(l.hits, k)
			} else {
				l.hits[k] = alive
			}
		}
	}
	return true
}

// Routes builds the full router. View-dependent handlers live in the
// sibling files.
func (s *Server) Routes() *chi.Mux {
	r := chi.NewRouter()
	r.Use(s.hostMiddleware, s.sslMiddleware, s.localeMiddleware, s.sessionMiddleware, s.csrfMiddleware)

	r.Get("/up", s.handleUp)
	r.Get("/manifest", s.handleManifest)
	r.Get("/service-worker", s.handleServiceWorker)
	r.Handle("/css/*", http.StripPrefix("/css/", http.FileServer(http.Dir(s.WebDir+"/css"))))
	r.Handle("/js/*", http.StripPrefix("/js/", http.FileServer(http.Dir(s.WebDir+"/js"))))
	for _, name := range []string{"icon.svg", "icon.png", "apple-touch-icon.png", "robots.txt"} {
		r.Get("/"+name, s.handleStaticFile(name))
	}

	r.Get("/signup", s.handleSignupNew)
	r.Post("/signup", s.handleSignupCreate)
	r.Get("/login", s.handleLoginNew)
	r.Post("/login", s.handleLoginCreate)
	r.Post("/locale", s.handleLocale)
	r.Get("/login/kura", s.handleKuraStart)
	r.Get("/login/kura/callback", s.handleKuraCallback)
	r.Delete("/logout", s.handleLogout)
	r.Post("/logout", s.handleLogout)

	r.Get("/unlock", s.handleUnlockNew)
	r.Post("/unlock", s.handleUnlockCreate)
	r.Post("/lock", s.handleLock)
	r.Get("/password/edit", s.handlePasswordEdit)
	r.Patch("/password", s.handlePasswordUpdate)

	r.Route("/api_tokens", func(r chi.Router) {
		r.Use(s.requireAuth, s.requireUnlock)
		r.Get("/", s.handleTokensIndex)
		r.Post("/", s.handleTokensCreate)
		r.Delete("/{id}", s.handleTokensDestroy)
		r.Post("/{id}/delete", s.handleTokensDestroy)
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(s.requireAPIToken)
		r.Get("/notes", s.handleAPINotesIndex)
		r.Post("/notes", s.handleAPINotesCreate)
		r.Get("/notes/{id}", s.handleAPINotesShow)
		r.Patch("/notes/{id}", s.handleAPINotesUpdate)
		r.Put("/notes/{id}", s.handleAPINotesUpdate)
		r.Delete("/notes/{id}", s.handleAPINotesDestroy)
		r.Get("/folders", s.handleAPIFoldersIndex)
		r.Patch("/folders", s.handleAPIFoldersUpdate)
		r.Delete("/folders", s.handleAPIFoldersDestroy)
	})

	r.Route("/notes", func(r chi.Router) {
		r.Use(s.requireAuth, s.requireUnlock)
		r.Get("/", s.handleNotesIndex)
		r.Post("/", s.handleNotesCreate)
		r.Get("/export", s.handleNotesExport)
		r.Post("/import", s.handleNotesImport)
		r.Patch("/folder", s.handleFolderRename)
		r.Delete("/folder", s.handleFolderDestroy)
		r.Get("/{id}", s.handleNotesShow)
		r.Patch("/{id}", s.handleNotesUpdate)
		r.Delete("/{id}", s.handleNotesDestroy)
		r.Post("/{id}/share", s.handleShareCreate)
		r.Patch("/{id}/share", s.handleShareUpdate)
		r.Delete("/{id}/share", s.handleShareDestroy)
	})
	r.Get("/s/{token}", s.handleSharedShow)

	r.With(s.requireAuth, s.requireUnlock).Get("/", s.handleNotesIndex)
	return r
}
