package handler

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aquasp/kurapeople/internal/i18n"
)

const localeCookie = "kura_locale"

func localeCookieValue(r *http.Request) string {
	c, err := r.Cookie(localeCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

func (s *Server) handleLocale(w http.ResponseWriter, r *http.Request) {
	lang := strings.ToLower(strings.TrimSpace(r.FormValue("lang")))
	if lang != string(i18n.EN) && lang != string(i18n.PT) {
		http.Error(w, "bad locale", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     localeCookie,
		Value:    lang,
		Path:     "/",
		Domain:   sharedCookieDomain(s.Config.KuraHosts),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.secureCookies(r),
		MaxAge:   365 * 24 * 3600,
		Expires:  time.Now().Add(365 * 24 * time.Hour),
	})
	http.Redirect(w, r, sameOriginReturn(r), http.StatusSeeOther)
}

// sameOriginReturn sends the browser back to the page that submitted
// the switch. Off-site Referer values fall back to the app root.
func sameOriginReturn(r *http.Request) string {
	ref, err := url.Parse(r.Referer())
	if err != nil || !strings.EqualFold(ref.Host, r.Host) {
		return "/"
	}
	if ref.Path == "" || !strings.HasPrefix(ref.Path, "/") || strings.HasPrefix(ref.Path, "//") || strings.ContainsAny(ref.RequestURI(), "\\") {
		return "/"
	}
	return ref.RequestURI()
}
