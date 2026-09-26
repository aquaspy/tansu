package handler

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/aquasp/kuracalendar/internal/store"
)

type birthdaySyncBody struct {
	AccountSub string `json:"account_sub"`
	Email      string `json:"email"`
	SourceKey  string `json:"source_key"`
	Name       string `json:"name"`
	Emoji      string `json:"emoji"`
	Month      int    `json:"month"`
	Day        int    `json:"day"`
	Year       int    `json:"year"`
	Delete     bool   `json:"delete"`
}

// handleBirthdaySync accepts a birthday from Tansu People. The shared
// secret is the only credential; there is no browser session.
func (s *Server) handleBirthdaySync(w http.ResponseWriter, r *http.Request) {
	secret := s.Config.SyncSecret
	got := r.Header.Get("X-Kura-Sync")
	if secret == "" || len(got) != len(secret) ||
		subtle.ConstantTimeCompare([]byte(got), []byte(secret)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var body birthdaySyncBody
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	body.SourceKey = strings.TrimSpace(body.SourceKey)
	if body.SourceKey == "" || len(body.SourceKey) > 80 || strings.ContainsAny(body.SourceKey, " \r\n") {
		http.Error(w, "bad source", http.StatusBadRequest)
		return
	}
	var user *store.User
	if strings.TrimSpace(body.AccountSub) != "" {
		user, _ = s.Store.FindUserBySub(body.AccountSub)
	}
	if user == nil && strings.TrimSpace(body.Email) != "" {
		user, _ = s.Store.FindUserByEmail(body.Email)
	}
	if user == nil {
		http.Error(w, "no user", http.StatusNotFound)
		return
	}
	if body.Delete || body.Month == 0 || body.Day == 0 {
		if err := s.Store.DeleteSyncedBirthday(user.ID, body.SourceKey); err != nil {
			http.Error(w, "unavailable", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	in := store.BirthdayInput{
		Name:  body.Name,
		Month: strconv.Itoa(body.Month),
		Day:   strconv.Itoa(body.Day),
		Emoji: body.Emoji,
	}
	if body.Year > 0 {
		in.Year = strconv.Itoa(body.Year)
	}
	if _, errs, err := s.Store.UpsertSyncedBirthday(user.ID, body.SourceKey, in); err != nil {
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	} else if len(errs) > 0 {
		http.Error(w, "invalid", http.StatusUnprocessableEntity)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
