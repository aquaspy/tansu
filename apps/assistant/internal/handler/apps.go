package handler

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aquasp/kurachat/internal/i18n"
	"github.com/aquasp/kurachat/internal/store"
	"github.com/aquasp/kurachat/internal/suite"
	"github.com/aquasp/kurachat/internal/views"
	"github.com/go-chi/chi/v5"
)

var suiteApps = []string{"notes", "calendar", "spend", "people"}

func (s *Server) appBase(name string) string {
	switch name {
	case "notes":
		return s.Config.NotesURL
	case "calendar":
		return s.Config.CalendarURL
	case "spend":
		return s.Config.SpendURL
	case "people":
		return s.Config.PeopleURL
	}
	return ""
}

func (s *Server) knownApp(name string) bool {
	for _, a := range suiteApps {
		if a == name {
			return true
		}
	}
	return false
}

func (s *Server) handleApps(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	p := s.page(w, r, pTitle(r, "titles.apps"), "auth-body")
	render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.AppsPage(p, s.appRows(r, user))))
}

func (s *Server) appRows(r *http.Request, user *store.User) []views.AppRow {
	links, _ := s.Store.ListAppLinks(user.ID)
	by := map[string]store.AppLink{}
	for _, l := range links {
		by[l.App] = l
	}
	var rows []views.AppRow
	for _, name := range suiteApps {
		row := views.AppRow{Name: name, Label: i18n.T(LocaleOf(r), "apps."+name)}
		base := s.appBase(name)
		if base == "" {
			row.State = "unconfigured"
			rows = append(rows, row)
			continue
		}
		if len(s.Config.AppsKey) != 32 {
			row.State = "nokey"
			rows = append(rows, row)
			continue
		}
		link, ok := by[name]
		if !ok {
			row.State = "off"
			rows = append(rows, row)
			continue
		}
		if _, err := suite.Decrypt(s.Config.AppsKey, link.Token); err != nil {
			row.State = "broken"
			rows = append(rows, row)
			continue
		}
		row.State = "linked"
		row.Email = link.Email
		row.Prefix = link.TokenPrefix
		rows = append(rows, row)
	}
	return rows
}

func (s *Server) handleAppConnect(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	app := chi.URLParam(r, "app")
	if !s.knownApp(app) || s.appBase(app) == "" || len(s.Config.AppsKey) != 32 {
		http.Redirect(w, r, "/apps", http.StatusSeeOther)
		return
	}
	if !s.Limiter.Allow("app-connect:"+itoa64(user.ID), 10, 3*time.Minute) {
		http.Error(w, i18n.T(l, "auth.too_many"), http.StatusTooManyRequests)
		return
	}
	state, verifier, err := store.NewConnectState()
	if err != nil || s.Store.SaveAppConnect(user.ID, app, state, verifier) != nil {
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{}
	q.Set("state", state)
	q.Set("redirect_uri", s.publicOrigin(r)+"/apps/"+app+"/callback")
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(sum[:]))
	q.Set("code_challenge_method", "S256")
	http.Redirect(w, r, s.appBase(app)+"/agent/connect?"+q.Encode(), http.StatusSeeOther)
}

func (s *Server) publicOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || s.Config.ForceSSL {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *Server) handleAppCallback(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	app := chi.URLParam(r, "app")
	back := "/apps"
	if !s.knownApp(app) || !s.Limiter.Allow("app-callback:"+itoa64(user.ID), 20, 3*time.Minute) {
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	flash := func(alert string) {
		if sess := SessionOf(r); sess != nil {
			_ = s.Store.SetFlash(sess.ID, "", alert)
		}
		http.Redirect(w, r, back, http.StatusSeeOther)
	}
	gotApp, verifier, err := s.Store.ConsumeAppConnect(r.URL.Query().Get("state"), user.ID, store.AppConnectTTL)
	if err != nil || gotApp != app {
		flash(i18n.T(l, "apps.bad_return"))
		return
	}
	body, _ := json.Marshal(map[string]string{
		"code": r.URL.Query().Get("code"), "code_verifier": verifier,
	})
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.appBase(app)+"/agent/exchange", bytes.NewReader(body))
	if err != nil {
		flash(i18n.T(l, "apps.bad_return"))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}).Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		flash(i18n.T(l, "apps.bad_return"))
		return
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	resp.Body.Close()
	var got struct {
		Token      string `json:"token"`
		Email      string `json:"email"`
		AccountSub string `json:"account_sub"`
	}
	if json.Unmarshal(raw, &got) != nil || got.Token == "" {
		flash(i18n.T(l, "apps.bad_return"))
		return
	}
	if !suite.SameAccount(user.AccountSub, user.Email, got.AccountSub, got.Email) {
		revokeSibling(s.appBase(app), got.Token)
		flash(i18n.T(l, "apps.mismatch"))
		return
	}
	blob, err := suite.Encrypt(s.Config.AppsKey, []byte(got.Token))
	if err != nil {
		revokeSibling(s.appBase(app), got.Token)
		flash(i18n.T(l, "apps.bad_return"))
		return
	}
	prefix := got.Token
	if len(prefix) > 4 {
		prefix = prefix[len(prefix)-4:]
	}
	if err := s.Store.UpsertAppLink(store.AppLink{
		UserID: user.ID, App: app, Token: blob, TokenPrefix: prefix,
		Email: got.Email, AccountSub: got.AccountSub,
	}); err != nil {
		revokeSibling(s.appBase(app), got.Token)
		flash(i18n.T(l, "apps.bad_return"))
		return
	}
	if sess := SessionOf(r); sess != nil {
		_ = s.Store.SetFlash(sess.ID, i18n.T(l, "apps.linked", "email", got.Email), "")
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (s *Server) handleAppDisconnect(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	app := chi.URLParam(r, "app")
	if !s.knownApp(app) {
		http.Redirect(w, r, "/apps", http.StatusSeeOther)
		return
	}
	links, _ := s.Store.ListAppLinks(user.ID)
	var token string
	var hadLink bool
	for _, link := range links {
		if link.App != app {
			continue
		}
		hadLink = true
		if raw, err := suite.Decrypt(s.Config.AppsKey, link.Token); err == nil {
			token = string(raw)
		}
	}
	_ = s.Store.DeleteAppLink(user.ID, app)
	notice := i18n.T(l, "apps.disconnected")
	switch {
	case hadLink && token == "":
		notice = i18n.T(l, "apps.revoke_remote")
	case token != "" && s.appBase(app) != "" && !revokeSibling(s.appBase(app), token):
		notice = i18n.T(l, "apps.revoke_remote")
	}
	if sess := SessionOf(r); sess != nil {
		_ = s.Store.SetFlash(sess.ID, notice, "")
	}
	http.Redirect(w, r, "/apps", http.StatusSeeOther)
}

func revokeSibling(base, token string) bool {
	req, err := http.NewRequest(http.MethodDelete, base+"/api/v1/token", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusNoContent
}

func (s *Server) handleToolDecision(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	convID, err := convID(r)
	if err != nil {
		s.notFound(w, r)
		return
	}
	conv, err := s.Store.FindConversation(user.ID, convID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if !conv.AllowsTools() {
		http.Redirect(w, r, "/conversations/"+itoa64(conv.ID), http.StatusSeeOther)
		return
	}
	mid, err := strconv.ParseInt(chi.URLParam(r, "mid"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	msg, err := s.Store.GetMessage(mid)
	if err != nil || msg.ConversationID != conv.ID || msg.Status != store.StatusConfirming {
		http.Redirect(w, r, "/conversations/"+itoa64(conv.ID), http.StatusSeeOther)
		return
	}
	callID := r.FormValue("call_id")
	var doc struct {
		Calls []map[string]any `json:"calls"`
	}
	_ = json.Unmarshal([]byte(msg.ToolTrace), &doc)
	found := false
	for _, c := range doc.Calls {
		if c["id"] == callID && c["status"] == "needs_confirm" {
			found = true
			if r.FormValue("decision") == "approve" {
				c["status"] = "approved"
			} else {
				c["status"] = "cancelled"
			}
		}
	}
	if !found {
		http.Redirect(w, r, "/conversations/"+itoa64(conv.ID), http.StatusSeeOther)
		return
	}
	raw, _ := json.Marshal(doc)
	_ = s.Store.SetToolTrace(msg.ID, string(raw))
	back := "/conversations/" + itoa64(conv.ID)
	if r.FormValue("decision") != "approve" {
		text := strings.TrimSpace(msg.Content)
		if text != "" {
			text += "\n\n"
		}
		text += i18n.T(l, "chat.action_cancelled")
		_, _ = s.Store.SetMessageStatus(msg.ID, store.StatusConfirming, store.StatusComplete, text)
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	ok, err := s.Store.ClaimConfirm(msg.ID)
	if err != nil || !ok {
		if sess := SessionOf(r); sess != nil {
			_ = s.Store.SetFlash(sess.ID, "", i18n.T(l, "chat.in_flight"))
		}
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	s.Chat.RunAsync(msg.ID, l)
	http.Redirect(w, r, back, http.StatusSeeOther)
}
