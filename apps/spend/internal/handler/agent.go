package handler

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aquasp/kuraspend/internal/i18n"
	"github.com/aquasp/kuraspend/internal/store"
	"github.com/aquasp/kuraspend/internal/views"
)

const (
	agentApp          = "spend"
	agentReturnCookie = "kura_agent_return"
)

// AssistantRedirectOK reports whether raw is exactly
// {KURA_ASSISTANT_URL}/apps/<app>/callback. A trailing slash on the
// configured origin is ignored. Query, fragment, and userinfo are rejected.
func AssistantRedirectOK(configured, raw, app string) bool {
	configured = strings.TrimRight(strings.TrimSpace(configured), "/")
	if configured == "" || strings.TrimSpace(raw) == "" {
		return false
	}
	base, err := url.Parse(configured)
	got, err2 := url.Parse(raw)
	if err != nil || err2 != nil || base.Scheme == "" || base.Host == "" {
		return false
	}
	if !strings.EqualFold(base.Scheme, got.Scheme) || !strings.EqualFold(base.Host, got.Host) {
		return false
	}
	if got.User != nil || got.RawQuery != "" || got.Fragment != "" {
		return false
	}
	return strings.TrimRight(got.Path, "/") == "/apps/"+app+"/callback"
}

func validAgentState(s string) bool {
	if len(s) < 16 || len(s) > 128 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

func validChallenge(method, challenge string) bool {
	if method != "S256" || len(challenge) < 40 || len(challenge) > 128 {
		return false
	}
	for _, r := range challenge {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

func pkceMatch(verifier, challenge string) bool {
	if verifier == "" || challenge == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	got := base64.RawURLEncoding.EncodeToString(sum[:])
	if len(got) != len(challenge) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(challenge)) == 1
}

func safeAgentReturn(raw string) string {
	if strings.Contains(raw, "\\") || strings.Contains(raw, "//") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host != "" || u.Path != "/agent/connect" || u.Opaque != "" {
		return ""
	}
	return u.RequestURI()
}

func (s *Server) rememberAgentReturn(w http.ResponseWriter, r *http.Request) {
	safe := safeAgentReturn(r.URL.RequestURI())
	if safe == "" {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     agentReturnCookie,
		Value:    url.QueryEscape(safe),
		Path:     "/",
		MaxAge:   600,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.secureCookies(r),
	})
}

// takeAgentReturn clears the return cookie and yields a same-app /agent/connect path.
func (s *Server) takeAgentReturn(w http.ResponseWriter, r *http.Request) string {
	c, err := r.Cookie(agentReturnCookie)
	clearCookie(w, r, s.Config, agentReturnCookie)
	if err != nil {
		return "/"
	}
	v, _ := url.QueryUnescape(c.Value)
	if safe := safeAgentReturn(v); safe != "" {
		return safe
	}
	return "/"
}

func (s *Server) agentParamsBad(redirectURI, state, challenge, method string) bool {
	return !AssistantRedirectOK(s.Config.KuraAssistantURL, redirectURI, agentApp) ||
		!validAgentState(state) || !validChallenge(method, challenge)
}

func (s *Server) handleAgentConnect(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	if s.Config.KuraAssistantURL == "" {
		http.Error(w, "agent connect disabled", http.StatusNotFound)
		return
	}
	redirectURI := r.URL.Query().Get("redirect_uri")
	state := r.URL.Query().Get("state")
	challenge := r.URL.Query().Get("code_challenge")
	method := r.URL.Query().Get("code_challenge_method")
	if s.agentParamsBad(redirectURI, state, challenge, method) {
		p := s.page(w, r, pTitle(r, "titles.agent"), "auth-body")
		p.Alert = i18n.T(l, "agent.bad_request")
		render(w, r, http.StatusBadRequest, views.Layout(p, views.NoHead(), views.AgentError(p)))
		return
	}
	if UserOf(r) == nil {
		s.rememberAgentReturn(w, r)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if !sessionUsable(r) {
		s.rememberAgentReturn(w, r)
		http.Redirect(w, r, "/unlock", http.StatusSeeOther)
		return
	}
	user := UserOf(r)
	if !s.Limiter.Allow("agent-connect:"+itoa64(user.ID), 10, 3*time.Minute) {
		p := s.page(w, r, pTitle(r, "titles.agent"), "auth-body")
		p.Alert = i18n.T(l, "auth.too_many")
		render(w, r, http.StatusTooManyRequests, views.Layout(p, views.NoHead(), views.AgentError(p)))
		return
	}
	p := s.page(w, r, pTitle(r, "titles.agent"), "auth-body")
	render(w, r, http.StatusOK, views.Layout(p, views.NoHead(),
		views.AgentConfirm(p, user.Email, redirectURI, state, challenge)))
}

func (s *Server) handleAgentConnectCreate(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	if s.Config.KuraAssistantURL == "" {
		http.Error(w, "agent connect disabled", http.StatusNotFound)
		return
	}
	if UserOf(r) == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if !sessionUsable(r) {
		http.Redirect(w, r, "/unlock", http.StatusSeeOther)
		return
	}
	user := UserOf(r)
	redirectURI := r.FormValue("redirect_uri")
	state := r.FormValue("state")
	challenge := r.FormValue("code_challenge")
	if s.agentParamsBad(redirectURI, state, challenge, "S256") {
		p := s.page(w, r, pTitle(r, "titles.agent"), "auth-body")
		p.Alert = i18n.T(l, "agent.bad_request")
		render(w, r, http.StatusBadRequest, views.Layout(p, views.NoHead(), views.AgentError(p)))
		return
	}
	if !s.Limiter.Allow("agent-connect:"+itoa64(user.ID), 10, 3*time.Minute) {
		p := s.page(w, r, pTitle(r, "titles.agent"), "auth-body")
		p.Alert = i18n.T(l, "auth.too_many")
		render(w, r, http.StatusTooManyRequests, views.Layout(p, views.NoHead(), views.AgentError(p)))
		return
	}
	_, rawToken, err := s.Store.MintAssistantToken(user.ID)
	if err != nil {
		p := s.page(w, r, pTitle(r, "titles.agent"), "auth-body")
		key := "auth.too_many"
		if errors.Is(err, store.ErrTokenTooMany) {
			key = "tokens.too_many"
		}
		p.Alert = i18n.T(l, key)
		render(w, r, http.StatusUnprocessableEntity, views.Layout(p, views.NoHead(), views.AgentError(p)))
		return
	}
	code, digest, err := store.NewAgentCode()
	if err != nil {
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	if err := s.Store.SaveAgentGrant(user.ID, digest, challenge, rawToken, store.AgentGrantTTL); err != nil {
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	q := url.Values{}
	q.Set("code", code)
	q.Set("state", state)
	http.Redirect(w, r, redirectURI+"?"+q.Encode(), http.StatusSeeOther)
}

func (s *Server) handleAgentExchange(w http.ResponseWriter, r *http.Request) {
	if s.Config.KuraAssistantURL == "" {
		http.Error(w, "agent connect disabled", http.StatusNotFound)
		return
	}
	if !s.Limiter.Allow("agent-exchange:"+clientIP(r), 60, time.Minute) {
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	var body struct {
		Code         string `json:"code"`
		CodeVerifier string `json:"code_verifier"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<16))
	if err := dec.Decode(&body); err != nil || body.Code == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request")
		return
	}
	grant, err := s.Store.ConsumeAgentGrant(store.DigestAgentCode(body.Code))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if !pkceMatch(body.CodeVerifier, grant.Challenge) {
		writeAPIError(w, http.StatusBadRequest, "bad_request")
		return
	}
	user, err := s.Store.FindUser(grant.UserID)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request")
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{
		"token":       grant.Token,
		"email":       user.Email,
		"account_sub": user.AccountSub,
	})
}

func (s *Server) handleAPITokenDestroy(w http.ResponseWriter, r *http.Request) {
	tok := apiTokenOf(r)
	if tok == nil {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	_ = s.Store.DeleteToken(tok.UserID, tok.ID)
	w.WriteHeader(http.StatusNoContent)
}
