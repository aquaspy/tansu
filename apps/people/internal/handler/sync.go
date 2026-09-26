package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/aquasp/kurapeople/internal/store"
)

// pushBirthday tells Tansu Calendar about one card. A missing calendar,
// a missing user over there, or a network error does not fail the save:
// People stays usable on its own.
func (s *Server) pushBirthday(user *store.User, person *store.Person, del bool) {
	if user == nil || person == nil || !s.Config.SyncEnabled() {
		return
	}
	payload := map[string]any{
		"account_sub": user.AccountSub,
		"email":       user.Email,
		"source_key":  "people:" + strconv.FormatInt(person.ID, 10),
		"name":        person.Name,
		"emoji":       person.Emoji,
		"month":       person.BirthMonth,
		"day":         person.BirthDay,
		"year":        person.BirthYear,
		"delete":      del || person.BirthMonth == 0 || person.BirthDay == 0,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Config.CalendarURL+"/sync/birthdays", bytes.NewReader(raw))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Kura-Sync", s.Config.SyncSecret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

func (s *Server) pushBirthdayFor(userID int64, person *store.Person, del bool) {
	user, err := s.Store.FindUser(userID)
	if err != nil {
		return
	}
	s.pushBirthday(user, person, del)
}
