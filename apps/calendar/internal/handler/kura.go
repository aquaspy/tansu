package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aquasp/kuracalendar/internal/i18n"
	"github.com/aquasp/kuracalendar/internal/store"
	"github.com/aquasp/kuracalendar/internal/views"
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
	verifier, err := s.Store.ConsumeKuraLogin(r.URL.Query().Get("state"), store.KuraLoginTTL)
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
		if existing, err := s.Store.FindUserByEmail(profile.Email); err == nil {
			// Same email, first SSO: link the existing account.
			if err := s.Store.SetUserSub(existing.ID, profile.Sub); err != nil {
				fail(http.StatusUnprocessableEntity, "auth.kura_failed")
				return
			}
			user = existing
			user.AccountSub = profile.Sub
		}
	}
	if user == nil {
		if !s.Config.SignupEnabled {
			fail(http.StatusUnprocessableEntity, "auth.kura_signup_closed")
			return
		}
		// Provision: the digest is unusable, password login stays closed.
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
	http.Redirect(w, r, "/", http.StatusSeeOther)
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
