package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aquasp/kuraemail/internal/mail"
	"github.com/aquasp/kuraemail/internal/secret"
	"github.com/aquasp/kuraemail/internal/store"
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

func (s *Server) requireAPIToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	_, _ = w.Write([]byte(`{"error":"` + code + `"}`))
}

func writeAPIErrors(w http.ResponseWriter, status int, errs []string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	b, _ := json.Marshal(map[string][]string{"errors": errs})
	_, _ = w.Write(b)
}

func writeAPIJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	b, _ := json.Marshal(v)
	_, _ = w.Write(b)
}

func (s *Server) handleAPIAccountsIndex(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListMailboxes(apiUserOf(r).ID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "unavailable")
		return
	}
	out := make([]any, 0, len(list))
	for _, m := range list {
		out = append(out, publicMailbox(m))
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"accounts": out})
}

func (s *Server) handleAPIAccountsCreate(w http.ResponseWriter, r *http.Request) {
	in, err := decodeMailbox(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request")
		return
	}
	box, err := s.saveNewMailbox(apiUserOf(r).ID, in)
	if err != nil {
		if errors.Is(err, secret.ErrKey) {
			writeAPIError(w, http.StatusUnprocessableEntity, "secrets_key")
			return
		}
		writeAPIErrors(w, http.StatusUnprocessableEntity, strings.Split(err.Error(), ", "))
		return
	}
	writeAPIJSON(w, http.StatusCreated, map[string]any{"account": publicMailbox(box)})
}

func (s *Server) handleAPIAccountsShow(w http.ResponseWriter, r *http.Request) {
	box, ok := s.apiBox(w, r)
	if !ok {
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"account": publicMailbox(box)})
}

func (s *Server) handleAPIAccountsUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	in, err := decodeMailbox(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request")
		return
	}
	box, err := s.saveMailbox(apiUserOf(r).ID, id, in)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "not_found")
			return
		}
		if errors.Is(err, secret.ErrKey) {
			writeAPIError(w, http.StatusUnprocessableEntity, "secrets_key")
			return
		}
		writeAPIErrors(w, http.StatusUnprocessableEntity, strings.Split(err.Error(), ", "))
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"account": publicMailbox(box)})
}

func (s *Server) handleAPIAccountsDestroy(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.Store.DeleteMailbox(apiUserOf(r).ID, id); err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAPIFolders(w http.ResponseWriter, r *http.Request) {
	_, creds, ok := s.apiCreds(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	folders, err := s.Mail.Folders(ctx, creds)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "mail_unavailable")
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"folders": folders})
}

func (s *Server) handleAPIMessages(w http.ResponseWriter, r *http.Request) {
	box, creds, ok := s.apiCreds(w, r)
	if !ok {
		return
	}
	page := 1
	if n, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && n > 0 {
		page = n
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	list, err := s.Mail.List(ctx, box.ID, creds, r.URL.Query().Get("folder"), r.URL.Query().Get("q"), page)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "mail_unavailable")
		return
	}
	writeAPIJSON(w, http.StatusOK, list)
}

func (s *Server) handleAPIMessage(w http.ResponseWriter, r *http.Request) {
	_, creds, ok := s.apiCreds(w, r)
	if !ok {
		return
	}
	uid, ok := pathUID(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	msg, err := s.Mail.Read(ctx, creds, r.URL.Query().Get("folder"), uid)
	if err != nil {
		if errors.Is(err, mail.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "not_found")
			return
		}
		writeAPIError(w, http.StatusBadGateway, "mail_unavailable")
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"message": publicMessage(msg)})
}

func (s *Server) handleAPITrash(w http.ResponseWriter, r *http.Request) {
	s.apiMove(w, r, true)
}

func (s *Server) handleAPIDelete(w http.ResponseWriter, r *http.Request) {
	s.apiMove(w, r, false)
}

func (s *Server) apiMove(w http.ResponseWriter, r *http.Request, trash bool) {
	box, creds, ok := s.apiCreds(w, r)
	if !ok {
		return
	}
	uid, ok := pathUID(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	var err error
	if trash {
		err = s.Mail.Trash(ctx, box.ID, creds, r.URL.Query().Get("folder"), uid)
	} else {
		err = s.Mail.Delete(ctx, box.ID, creds, r.URL.Query().Get("folder"), uid)
	}
	if err != nil {
		if errors.Is(err, mail.ErrNoTrash) {
			writeAPIError(w, http.StatusUnprocessableEntity, "no_trash")
			return
		}
		writeAPIError(w, http.StatusBadGateway, "mail_unavailable")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAPIAttachment(w http.ResponseWriter, r *http.Request) {
	_, creds, ok := s.apiCreds(w, r)
	if !ok {
		return
	}
	uid, ok := pathUID(w, r)
	if !ok {
		return
	}
	part, err := strconv.Atoi(chi.URLParam(r, "part"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	name, mime, body, err := s.Mail.Attachment(ctx, creds, r.URL.Query().Get("folder"), uid, part)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	if mime == "" {
		mime = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+strings.ReplaceAll(name, "\"", "")+"\"")
	_, _ = w.Write(body)
}

func (s *Server) handleAPIPreview(w http.ResponseWriter, r *http.Request) {
	box, creds, ok := s.apiCreds(w, r)
	if !ok {
		return
	}
	_ = box
	msg, err := decodeOutgoing(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request")
		return
	}
	preview, err := s.Mail.Preview(creds, msg)
	if err != nil {
		writeAPIErrors(w, http.StatusUnprocessableEntity, []string{err.Error()})
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"preview": preview, "sent": false})
}

func (s *Server) handleAPISend(w http.ResponseWriter, r *http.Request) {
	box, creds, ok := s.apiCreds(w, r)
	if !ok {
		return
	}
	msg, err := decodeOutgoing(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	if err := s.Mail.Send(ctx, box.ID, creds, msg); err != nil {
		writeAPIErrors(w, http.StatusBadGateway, []string{secret.Scrub(err, creds.Password)})
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"sent": true, "subject": msg.Subject})
}

func (s *Server) apiBox(w http.ResponseWriter, r *http.Request) (*store.Mailbox, bool) {
	id, ok := pathID(w, r)
	if !ok {
		return nil, false
	}
	box, err := s.Store.FindMailbox(apiUserOf(r).ID, id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return nil, false
	}
	return box, true
}

func (s *Server) apiCreds(w http.ResponseWriter, r *http.Request) (*store.Mailbox, mail.Creds, bool) {
	id, ok := pathID(w, r)
	if !ok {
		return nil, mail.Creds{}, false
	}
	box, creds, err := s.openBox(apiUserOf(r).ID, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeAPIError(w, http.StatusNotFound, "not_found")
			return nil, mail.Creds{}, false
		}
		writeAPIError(w, http.StatusUnprocessableEntity, "secrets_key")
		return nil, mail.Creds{}, false
	}
	return box, creds, true
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return 0, false
	}
	return id, true
}

func pathUID(w http.ResponseWriter, r *http.Request) (uint32, bool) {
	uid, ok := parseUID(chi.URLParam(r, "uid"))
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return 0, false
	}
	return uid, true
}

func publicMessage(m *mail.Message) map[string]any {
	return map[string]any{
		"uid":         m.UID,
		"from":        m.From,
		"to":          m.To,
		"cc":          m.Cc,
		"subject":     m.Subject,
		"date":        rfc(m.Date),
		"seen":        m.Seen,
		"message_id":  m.MessageID,
		"in_reply_to": m.InReplyTo,
		"references":  m.References,
		"text":        m.Text,
		"attachments": m.Attachments,
	}
}

func rfc(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func decodeOutgoing(r *http.Request) (mail.Outgoing, error) {
	var body struct {
		To         []string `json:"to"`
		Cc         []string `json:"cc"`
		Bcc        []string `json:"bcc"`
		Subject    string   `json:"subject"`
		Text       string   `json:"text"`
		Body       string   `json:"body"`
		InReplyTo  string   `json:"in_reply_to"`
		References string   `json:"references"`
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(&body); err != nil {
		return mail.Outgoing{}, err
	}
	text := body.Body
	if text == "" {
		text = body.Text
	}
	return mail.Outgoing{
		To: body.To, Cc: body.Cc, Bcc: body.Bcc,
		Subject: body.Subject, Body: text,
		InReplyTo: body.InReplyTo, References: body.References,
	}, nil
}
