package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/aquasp/kuraspend/internal/store"
)

// pushPaymentDay tells Tansu Calendar about one reminder. A missing
// calendar, a missing user over there, or a network error does not fail
// the save: Spend stays usable on its own. Inactive rows are removed.
// Subscriptions are not pushed.
func (s *Server) pushPaymentDay(user *store.User, day *store.PaymentDay, del bool) {
	if user == nil || day == nil || !s.Config.SyncEnabled() {
		return
	}
	payload := map[string]any{
		"account_sub": user.AccountSub,
		"email":       user.Email,
		"source_key":  "spend:" + strconv.FormatInt(day.ID, 10),
		"title":       day.Title,
		"notes":       day.Notes,
		"due_day":     day.DueDay,
		"delete":      del || !day.Active,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Config.CalendarURL+"/sync/payment_days", bytes.NewReader(raw))
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
