package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aquasp/kurapeople/internal/i18n"
	"github.com/aquasp/kurapeople/internal/store"
	"github.com/aquasp/kurapeople/internal/views"
	"golang.org/x/oauth2"
)

// kuraOAuth builds the client for Kura Account. The account speaks
// plain OAuth2 + PKCE (no OIDC discovery yet), so endpoints are fixed.
func (s *Server) kuraOAuth(r *http.Request) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     s.Config.KuraClientID,
		ClientSecret: s.Config.KuraClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:   s.Config.KuraAccountURL + "/authorize",
			TokenURL:  s.Config.KuraAccountURL + "/token",
			AuthStyle: oauth2.AuthStyleInHeader, // the account takes basic auth only
		},
		RedirectURL: s.publicBaseURL(r) + "/login/kura/callback",
		Scopes:      []string{"openid", "email"},
	}
}

// publicBaseURL is the URL the browser sees for this app: the first
// KURA_HOST in production, the request host in dev/test.
func (s *Server) publicBaseURL(r *http.Request) string {
	if len(s.Config.KuraHosts) > 0 {
		scheme := "http"
		if s.Config.ForceSSL {
			scheme = "https"
		}
		return scheme + "://" + s.Config.KuraHosts[0]
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// handleKuraStart begins SSO: mint state + PKCE verifier, stash them,
// and send the browser to the account's authorize page.
func (s *Server) handleKuraStart(w http.ResponseWriter, r *http.Request) {
	if !s.Config.AccountEnabled() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	// Already inside the app: the hub can point here every time without
	// minting another code.
	if UserOf(r) != nil && sessionUsable(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	state, err := randomHex(32)
	if err != nil {
		http.Error(w, "try again", http.StatusInternalServerError)
		return
	}
	verifier, err := randomHex(32)
	if err != nil {
		http.Error(w, "try again", http.StatusInternalServerError)
		return
	}
	if err := s.Store.CreateKuraLogin(state, verifier); err != nil {
		http.Error(w, "try again", http.StatusInternalServerError)
		return
	}
	s.setKuraStateCookie(w, r, state)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	url := s.kuraOAuth(r).AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", challenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"))
	http.Redirect(w, r, url, http.StatusSeeOther)
}

// kuraProfile is the subset of /oauth/userinfo this app needs.
type kuraProfile struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
}

// handleKuraCallback completes SSO: validate state (once), exchange the
// code, then log in by account sub — linking or provisioning a local
// user as needed.
func (s *Server) handleKuraCallback(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	fail := func(status int, key string) {
		p := s.page(w, r, pTitle(r, "titles.login"), "auth-body")
		p.Alert = i18n.T(l, key)
		render(w, r, status, views.Layout(p, views.NoHead(),
			views.LoginPage(p, "", s.Config.SignupEnabled)))
	}
	if !s.Config.AccountEnabled() {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if !s.Limiter.Allow("kura:"+clientIP(r), 20, 3*time.Minute) {
		fail(http.StatusTooManyRequests, "auth.too_many")
		return
	}
	if r.URL.Query().Get("error") != "" {
		fail(http.StatusUnprocessableEntity, "auth.kura_failed")
		return
	}
	// The state cookie binds the callback to the browser that started
	// the flow. A mismatch must not consume the pending login, so the
	// real browser can still finish.
	state := r.URL.Query().Get("state")
	if !kuraStateMatches(r, state) {
		fail(http.StatusUnprocessableEntity, "auth.kura_failed")
		return
	}
	clearCookie(w, r, s.Config, kuraStateCookie)
	verifier, err := s.Store.ConsumeKuraLogin(state, store.KuraLoginTTL)
	if err != nil {
		fail(http.StatusUnprocessableEntity, "auth.kura_failed")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	tok, err := s.kuraOAuth(r).Exchange(ctx, r.URL.Query().Get("code"),
		oauth2.SetAuthURLParam("code_verifier", verifier))
	if err != nil {
		fail(http.StatusUnprocessableEntity, "auth.kura_failed")
		return
	}
	profile, err := s.fetchKuraProfile(ctx, tok.AccessToken)
	if err != nil || profile.Sub == "" || !store.ValidEmail(profile.Email) {
		fail(http.StatusUnprocessableEntity, "auth.kura_failed")
		return
	}
	user, err := s.Store.FindUserBySub(profile.Sub)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		fail(http.StatusUnprocessableEntity, "auth.kura_failed")
		return
	}
	if user == nil {
		existing, findErr := s.Store.FindUserByEmail(profile.Email)
		switch {
		case findErr == nil && existing.AccountSub != "" && existing.AccountSub != profile.Sub:
			// Already linked to another Account subject. Recreating the
			// Account row (same email, new id) must not take over.
			fail(http.StatusUnprocessableEntity, "auth.kura_failed")
			return
		case findErr == nil && existing.AccountSub == "":
			// Same email, first SSO: link the existing local account.
			if err := s.Store.SetUserSub(existing.ID, profile.Sub); err != nil {
				fail(http.StatusUnprocessableEntity, "auth.kura_failed")
				return
			}
			existing.AccountSub = profile.Sub
			user = existing
		case findErr == nil:
			user = existing
		case !errors.Is(findErr, store.ErrNotFound):
			fail(http.StatusUnprocessableEntity, "auth.kura_failed")
			return
		}
	}
	if user == nil {
		// A completed Account SSO callback provisions the local user even
		// when SIGNUP_ENABLED is false. Public /signup and password
		// registration stay gated. The digest is unusable, so password
		// login stays closed for this user.
		digest, err := randomHex(32)
		if err != nil {
			fail(http.StatusUnprocessableEntity, "auth.kura_failed")
			return
		}
		user, err = s.Store.CreateUser(profile.Email, "!kura!"+digest)
		if err != nil {
			fail(http.StatusUnprocessableEntity, "auth.kura_failed")
			return
		}
		if err := s.Store.SetUserSub(user.ID, profile.Sub); err != nil {
			fail(http.StatusUnprocessableEntity, "auth.kura_failed")
			return
		}
		user.AccountSub = profile.Sub
	}
	sess, err := s.Store.CreateSession(user.ID)
	if err != nil {
		fail(http.StatusUnprocessableEntity, "auth.too_many")
		return
	}
	s.setSessionCookie(w, r, sess.ID)
	http.Redirect(w, r, s.takeAgentReturn(w, r), http.StatusSeeOther)
}

// kuraStateCookie is the browser binding for one SSO attempt.
// HttpOnly + SameSite=Lax: a foreign site can navigate the user to
// the callback, but it cannot attach this cookie.
const kuraStateCookie = "kura_sso_state"

func (s *Server) setKuraStateCookie(w http.ResponseWriter, r *http.Request, state string) {
	http.SetCookie(w, &http.Cookie{
		Name:     kuraStateCookie,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.secureCookies(r),
		MaxAge:   int(store.KuraLoginTTL.Seconds()),
		Expires:  time.Now().Add(store.KuraLoginTTL),
	})
}

func kuraStateMatches(r *http.Request, state string) bool {
	if state == "" || len(state) > 128 {
		return false
	}
	c, err := r.Cookie(kuraStateCookie)
	if err != nil || c.Value == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(state)) == 1
}

func (s *Server) fetchKuraProfile(ctx context.Context, accessToken string) (*kuraProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		s.Config.KuraAccountURL+"/userinfo", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("userinfo failed")
	}
	var profile kuraProfile
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, err
	}
	return &profile, nil
}
