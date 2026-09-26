package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aquasp/kuranotes/internal/i18n"
	"github.com/aquasp/kuranotes/internal/store"
	"github.com/go-chi/chi/v5"
)

const (
	apiUserKey  ctxKey = "api_user"
	apiTokenKey ctxKey = "api_token"
)

func apiUserOf(r *http.Request) *store.User {
	u, _ := r.Context().Value(apiUserKey).(*store.User)
	return u
}

func apiTokenOf(r *http.Request) *store.APIToken {
	t, _ := r.Context().Value(apiTokenKey).(*store.APIToken)
	return t
}

// requireAPIToken authenticates the Bearer token. It works while the app is
// locked, like the Rails API.
func (s *Server) requireAPIToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mirrors the Rails sub(/\ABearer\s+/i, ""): the prefix is
		// optional, so a bare token still authenticates.
		h := strings.TrimSpace(r.Header.Get("Authorization"))
		if len(h) > 6 && strings.EqualFold(h[:6], "bearer") && (h[6] == ' ' || h[6] == '\t') {
			h = strings.TrimSpace(h[6:])
		}
		tok, err := s.Store.AuthenticateToken(h)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		user, err := s.Store.FindUser(tok.UserID)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		_ = s.Store.TouchTokenLastUsed(tok.ID)
		ctx := context.WithValue(r.Context(), apiTokenKey, tok)
		ctx = context.WithValue(ctx, apiUserKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func writeAPIError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

func writeAPIErrors(w http.ResponseWriter, status int, errs []string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string][]string{"errors": errs})
}

func writeAPIJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func apiNote(n *store.Note) map[string]any {
	return map[string]any{
		"id":         n.ID,
		"title":      n.Title,
		"body":       n.Body,
		"preview":    n.Preview,
		"folder":     n.Folder,
		"shared":     n.Shared(),
		"created_at": n.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at": n.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func (s *Server) handleAPINotesIndex(w http.ResponseWriter, r *http.Request) {
	user := apiUserOf(r)
	limit := 50
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			limit = min(max(n, 1), 200)
		}
	}
	folder := r.URL.Query().Get("folder")
	if folder == "" {
		folder = "all"
	}
	rows, err := s.Store.ListNotesLimit(user.ID, folder, r.URL.Query().Get("q"), limit)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "unavailable")
		return
	}
	out := make([]any, 0, len(rows))
	for _, row := range rows {
		// The index returns full bodies, so fetch each note.
		n, err := s.Store.FindNote(user.ID, row.ID)
		if err != nil {
			continue
		}
		out = append(out, apiNote(n))
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"notes": out})
}

func (s *Server) handleAPINotesShow(w http.ResponseWriter, r *http.Request) {
	user := apiUserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	n, err := s.Store.FindNote(user.ID, id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"note": apiNote(n)})
}

// apiNoteParams accepts nested ({"note": {...}}) or flat ({"body": ...})
// params, like the Rails controller.
func apiNoteParams(r *http.Request) (body, folder string, hasBody, hasFolder bool) {
	var payload map[string]any
	_ = json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&payload)
	if payload == nil {
		// Fall back to form params for curl -d without a content type.
		_ = r.ParseForm()
		if v, ok := r.Form["body"]; ok && len(v) > 0 {
			return v[0], r.FormValue("folder"), true, r.Form.Has("folder")
		}
		return "", "", false, false
	}
	src := payload
	if nested, ok := payload["note"].(map[string]any); ok {
		src = nested
	}
	if v, ok := src["body"].(string); ok {
		body, hasBody = v, true
	}
	if v, ok := src["folder"].(string); ok {
		folder, hasFolder = v, true
	}
	return body, folder, hasBody, hasFolder
}

func (s *Server) apiWriteLimited(w http.ResponseWriter, r *http.Request) bool {
	tok := apiTokenOf(r)
	if !s.Limiter.Allow("api:"+strconv.FormatInt(tok.ID, 10), 60, time.Minute) {
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited")
		return false
	}
	return true
}

func (s *Server) handleAPINotesCreate(w http.ResponseWriter, r *http.Request) {
	if !s.apiWriteLimited(w, r) {
		return
	}
	user := apiUserOf(r)
	body, folder, _, _ := apiNoteParams(r)
	n, err := s.Store.CreateNote(user.ID, body, folder)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "unavailable")
		return
	}
	writeAPIJSON(w, http.StatusCreated, map[string]any{"note": apiNote(n)})
}

func (s *Server) handleAPINotesUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.apiWriteLimited(w, r) {
		return
	}
	user := apiUserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	current, err := s.Store.FindNote(user.ID, id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	body, folder, hasBody, hasFolder := apiNoteParams(r)
	if !hasBody {
		body = current.Body
	}
	if !hasFolder {
		folder = current.Folder
	}
	n, err := s.Store.UpdateNote(user.ID, id, body, folder)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"note": apiNote(n)})
}

func (s *Server) handleAPINotesDestroy(w http.ResponseWriter, r *http.Request) {
	user := apiUserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	if _, err := s.Store.FindNote(user.ID, id); err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	_ = s.Store.DeleteNote(user.ID, id)
	s.Store.ReclaimSpace()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAPIFoldersIndex(w http.ResponseWriter, r *http.Request) {
	user := apiUserOf(r)
	counts, err := s.Store.FolderCounts(user.ID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "unavailable")
		return
	}
	var names []string
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]any, 0, len(names))
	for _, name := range names {
		out = append(out, map[string]any{"name": name, "count": counts[name]})
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"folders": out})
}

func (s *Server) handleAPIFoldersUpdate(w http.ResponseWriter, r *http.Request) {
	user := apiUserOf(r)
	var payload map[string]string
	_ = json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&payload)
	if payload == nil {
		_ = r.ParseForm()
		payload = map[string]string{"from": r.FormValue("from"), "to": r.FormValue("to")}
	}
	to, allowed, err := s.Store.RenameFolder(user.ID, payload["from"], payload["to"])
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "unavailable")
		return
	}
	if !allowed {
		writeAPIErrors(w, http.StatusUnprocessableEntity,
			[]string{i18n.T(LocaleOf(r), "app.folder_rename_invalid")})
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"folder": to})
}

func (s *Server) handleAPIFoldersDestroy(w http.ResponseWriter, r *http.Request) {
	user := apiUserOf(r)
	key := r.URL.Query().Get("folder")
	if key == "" {
		// Also accept a JSON or form body.
		var payload map[string]string
		_ = json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&payload)
		if payload != nil {
			key = payload["folder"]
		}
	}
	if key == "" || key == "all" {
		writeAPIErrors(w, http.StatusUnprocessableEntity,
			[]string{i18n.T(LocaleOf(r), "api.invalid_folder")})
		return
	}
	count, err := s.Store.DeleteFolder(user.ID, key)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "unavailable")
		return
	}
	s.Store.ReclaimSpace()
	writeAPIJSON(w, http.StatusOK, map[string]any{"folder": key, "deleted": count})
}
