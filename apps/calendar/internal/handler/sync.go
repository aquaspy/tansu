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

type paymentDaySyncBody struct {
	AccountSub string `json:"account_sub"`
	Email      string `json:"email"`
	SourceKey  string `json:"source_key"`
	Title      string `json:"title"`
	Notes      string `json:"notes"`
	DueDay     int    `json:"due_day"`
	Delete     bool   `json:"delete"`
}

// authorizeSync checks the shared X-Kura-Sync secret. An empty secret
// disables the endpoints.
func (s *Server) authorizeSync(r *http.Request) bool {
	secret := s.Config.SyncSecret
	got := r.Header.Get("X-Kura-Sync")
	if secret == "" || len(got) != len(secret) ||
		subtle.ConstantTimeCompare([]byte(got), []byte(secret)) != 1 {
		return false
	}
	return true
}

// syncUser matches the Account subject first, then email, same as birthdays.
func (s *Server) syncUser(accountSub, email string) *store.User {
	var user *store.User
	if strings.TrimSpace(accountSub) != "" {
		user, _ = s.Store.FindUserBySub(accountSub)
	}
	if user == nil && strings.TrimSpace(email) != "" {
		user, _ = s.Store.FindUserByEmail(email)
	}
	return user
}

func validSourceKey(key string) bool {
	return key != "" && len(key) <= 80 && !strings.ContainsAny(key, " \r\n")
}

func readSyncJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(dest); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return false
	}
	return true
}

// handleBirthdaySync accepts a birthday from Tansu People. The shared
// secret is the only credential; there is no browser session.
func (s *Server) handleBirthdaySync(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeSync(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var body birthdaySyncBody
	if !readSyncJSON(w, r, &body) {
		return
	}
	body.SourceKey = strings.TrimSpace(body.SourceKey)
	if !validSourceKey(body.SourceKey) {
		http.Error(w, "bad source", http.StatusBadRequest)
		return
	}
	user := s.syncUser(body.AccountSub, body.Email)
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

// handlePaymentDaySync accepts a payment-day reminder from Tansu Spend.
// Same secret and user match as birthdays. delete, or due_day 0, removes
// the marker (including when Spend marks the row inactive).
func (s *Server) handlePaymentDaySync(w http.ResponseWriter, r *http.Request) {
	if !s.authorizeSync(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var body paymentDaySyncBody
	if !readSyncJSON(w, r, &body) {
		return
	}
	body.SourceKey = strings.TrimSpace(body.SourceKey)
	if !validSourceKey(body.SourceKey) {
		http.Error(w, "bad source", http.StatusBadRequest)
		return
	}
	user := s.syncUser(body.AccountSub, body.Email)
	if user == nil {
		http.Error(w, "no user", http.StatusNotFound)
		return
	}
	if body.Delete || body.DueDay == 0 {
		if err := s.Store.DeleteSyncedPaymentDay(user.ID, body.SourceKey); err != nil {
			http.Error(w, "unavailable", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if _, errs, err := s.Store.UpsertSyncedPaymentDay(user.ID, body.SourceKey, store.PaymentDayInput{
		Title: body.Title, DueDay: body.DueDay, Notes: body.Notes,
	}); err != nil {
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	} else if len(errs) > 0 {
		http.Error(w, "invalid", http.StatusUnprocessableEntity)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
