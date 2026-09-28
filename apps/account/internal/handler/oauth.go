package handler

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aquasp/kuraaccount/internal/store"
)

// ---------------------------------------------------------------------------
// First-party OAuth2: authorize + code + userinfo. No JWT, no refresh
// tokens, no consent screen, no dynamic registration in v1 — every
// client is ours, from the static KURA_CLIENTS_JSON registry.
// ---------------------------------------------------------------------------

// authorizeURL rebuilds the current authorize request for the login
// "next" hop.
func authorizeURL(r *http.Request) string {
	u := "/authorize"
	if r.URL.RawQuery != "" {
		u += "?" + r.URL.RawQuery
	}
	return u
}

func (s *Server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	clientID := strings.TrimSpace(q.Get("client_id"))
	redirectURI := strings.TrimSpace(q.Get("redirect_uri"))
	// Unknown client or redirect: plain 400, never a redirect (an
	// attacker must not launder codes through us).
	client, err := s.Store.FindClient(clientID)
	if err != nil || clientID == "" {
		http.Error(w, "unknown client", http.StatusBadRequest)
		return
	}
	if q.Get("response_type") != "code" || !client.AllowsRedirect(redirectURI) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	for _, scope := range strings.Fields(q.Get("scope")) {
		if scope != "openid" && scope != "email" {
			http.Error(w, "invalid scope", http.StatusBadRequest)
			return
		}
	}
	// PKCE S256 is mandatory, even for confidential clients.
	challenge := strings.TrimSpace(q.Get("code_challenge"))
	if challenge == "" || q.Get("code_challenge_method") != "S256" {
		http.Error(w, "code_challenge_method must be S256", http.StatusBadRequest)
		return
	}
	state := q.Get("state")
	if len(state) > 1024 {
		http.Error(w, "state too long", http.StatusBadRequest)
		return
	}
	// Login-gated + unlocked (same bar as the hub itself).
	user := UserOf(r)
	if user == nil {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(authorizeURL(r)), http.StatusSeeOther)
		return
	}
	if !sessionUsable(r) {
		http.Redirect(w, r, "/unlock?next="+url.QueryEscape(authorizeURL(r)), http.StatusSeeOther)
		return
	}
	if !s.Limiter.Allow("authcode:"+itoa64(user.ID), 60, time.Minute) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}
	code, err := s.Store.CreateAuthCode(client.ID, user.ID, redirectURI, challenge)
	if err != nil {
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	// First-party auto-approve: every registered client is ours, so no
	// consent screen in v1.
	dest, _ := url.Parse(redirectURI)
	params := dest.Query()
	params.Set("code", code)
	if state != "" {
		params.Set("state", state)
	}
	dest.RawQuery = params.Encode()
	http.Redirect(w, r, dest.String(), http.StatusSeeOther)
}

func writeOAuthError(w http.ResponseWriter, status int, code, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "error_description": desc})
}

func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeOAuthError(w, http.StatusMethodNotAllowed, "invalid_request", "POST required")
		return
	}
	// client_secret_basic only (no secrets in the body).
	id, secret, ok := r.BasicAuth()
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="token"`)
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "basic auth required")
		return
	}
	if !s.Limiter.Allow("token:"+id, 60, time.Minute) {
		writeOAuthError(w, http.StatusTooManyRequests, "rate_limited", "slow down")
		return
	}
	client, err := s.Store.AuthenticateClient(id, secret)
	if err != nil {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_client", "bad credentials")
		return
	}
	_ = r.ParseForm()
	if r.PostForm.Get("grant_type") != "authorization_code" {
		writeOAuthError(w, http.StatusBadRequest, "unsupported_grant_type", "authorization_code only")
		return
	}
	code, err := s.Store.RedeemAuthCode(
		r.PostForm.Get("code"), client.ID, r.PostForm.Get("redirect_uri"))
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "bad, used, or expired code")
		return
	}
	// PKCE: verifier must hash to the challenge (S256, constant shape
	// check first).
	verifier := r.PostForm.Get("code_verifier")
	sum := sha256.Sum256([]byte(verifier))
	if verifier == "" || len(verifier) > 128 ||
		base64.RawURLEncoding.EncodeToString(sum[:]) != code.Challenge {
		writeOAuthError(w, http.StatusBadRequest, "invalid_grant", "PKCE failed")
		return
	}
	raw, err := s.Store.CreateAccessToken(client.ID, code.UserID)
	if err != nil {
		writeOAuthError(w, http.StatusInternalServerError, "server_error", "unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": raw,
		"token_type":   "Bearer",
		"expires_in":   int(store.AccessTokenTTL / time.Second),
	})
}

func (s *Server) handleUserinfo(w http.ResponseWriter, r *http.Request) {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	raw := h
	if len(h) > 6 && strings.EqualFold(h[:6], "bearer") && (h[6] == ' ' || h[6] == '\t') {
		raw = strings.TrimSpace(h[6:])
	}
	userID, _, err := s.Store.FindAccessToken(raw)
	if err != nil {
		w.Header().Set("WWW-Authenticate", `Bearer realm="userinfo"`)
		writeOAuthError(w, http.StatusUnauthorized, "invalid_token", "bad or expired token")
		return
	}
	user, err := s.Store.FindUser(userID)
	if err != nil {
		writeOAuthError(w, http.StatusUnauthorized, "invalid_token", "unknown user")
		return
	}
	zone := user.Timezone
	if zone == "" {
		zone = "UTC"
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"sub":      itoa64(user.ID),
		"email":    user.Email,
		"zoneinfo": zone,
	})
}
