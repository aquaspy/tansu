package spend

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/aquasp/kuraspend/internal/store"
)

// ImportCap mirrors SpendImporter::CAP (per section).
const ImportCap = 500

// Import mirrors SpendImporter.call: it counts saved records, skipping rows
// that fail validation. Unknown shapes are errors, like the Rails rescue
// path that flashes import_invalid.
func Import(st *store.Store, userID int64, data []byte) (int, error) {
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return 0, err
	}
	if payload == nil {
		return 0, errors.New("not an object")
	}
	app, _ := payload["app"].(string)
	// "KuraSpend" is the pre-rebrand marker; old exports keep working.
	if app != "TansuSpend" && app != "KuraSpend" && !hasSpendRows(payload) {
		return 0, errors.New("not a TansuSpend export")
	}

	count := 0
	for _, row := range firstObjects(payload["subscriptions"]) {
		p := store.SubscriptionPatch{
			Title:    strField(row, "title"),
			Currency: strField(row, "currency"),
			Interval: strField(row, "interval"),
			Notes:    strField(row, "notes"),
		}
		if cents, ok := intField(row, "amount_cents"); ok {
			p.AmountCents = &cents
		}
		if raw, ok := dayField(row, "due_day"); ok {
			p.DueDay = &raw
		}
		if raw, ok := dayField(row, "billing_month"); ok {
			p.BillingMonth = &raw
		}
		if v, present := row["active"]; present {
			active := truthy(v)
			p.Active = &active
		}
		if _, fails, err := st.CreateSubscription(userID, p); err == nil && len(fails) == 0 {
			count++
		}
	}

	paymentRows := firstObjects(payload["payment_days"])
	if len(paymentRows) == 0 {
		paymentRows = firstObjects(payload["bills"])
	}
	for _, row := range paymentRows {
		p := store.PaymentDayPatch{
			Title: strField(row, "title"),
			Notes: strField(row, "notes"),
		}
		if raw, ok := paymentDueDay(row); ok {
			p.DueDay = &raw
		}
		if v, present := row["active"]; present {
			active := truthy(v)
			p.Active = &active
		}
		if _, fails, err := st.CreatePaymentDay(userID, p); err == nil && len(fails) == 0 {
			count++
		}
	}

	for _, row := range firstObjects(payload["expenses"]) {
		p := store.ExpensePatch{
			Title:    strField(row, "title"),
			Currency: strField(row, "currency"),
			SpentOn:  strField(row, "spent_on"),
			Category: strField(row, "category"),
			Notes:    strField(row, "notes"),
		}
		if cents, ok := intField(row, "amount_cents"); ok {
			p.AmountCents = &cents
		}
		if _, fails, err := st.CreateExpense(userID, p); err == nil && len(fails) == 0 {
			count++
		}
	}
	return count, nil
}

func hasSpendRows(payload map[string]any) bool {
	for _, key := range []string{"subscriptions", "payment_days", "bills", "expenses"} {
		if list, ok := payload[key].([]any); ok && len(list) > 0 {
			return true
		}
	}
	return false
}

func firstObjects(v any) []map[string]any {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []map[string]any
	for _, item := range list {
		if row, ok := item.(map[string]any); ok {
			out = append(out, row)
			if len(out) >= ImportCap {
				break
			}
		}
	}
	return out
}

// truthy mirrors SpendImporter.truthy?.
func truthy(value any) bool {
	if b, ok := value.(bool); ok {
		return b
	}
	if value == nil {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(stringify(value))) {
	case "0", "false", "no", "off":
		return false
	}
	return true
}

func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	default:
		return ""
	}
}

// strField always returns a pointer (missing reads as ""), mirroring the
// Rails .to_s calls.
func strField(row map[string]any, key string) *string {
	s := stringify(row[key])
	return &s
}

// intField truncates JSON numbers like the ActiveRecord integer cast.
func intField(row map[string]any, key string) (int64, bool) {
	switch v := row[key].(type) {
	case float64:
		return int64(v), true
	case string:
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			return n, true
		}
	case bool:
		if v {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// dayField keeps whole numbers and numeric strings; fractional values read
// as invalid downstream.
func dayField(row map[string]any, key string) (string, bool) {
	switch v := row[key].(type) {
	case float64:
		if v != math.Trunc(v) {
			return "invalid", true
		}
		return strconv.Itoa(int(v)), true
	case string:
		return v, true
	case bool:
		return strconv.FormatBool(v), true
	}
	return "", false
}

// paymentDueDay mirrors payment_day_attrs: due_day.presence, else the day
// parsed from due_on.
func paymentDueDay(row map[string]any) (string, bool) {
	if v, present := row["due_day"]; present && v != nil {
		if s, ok := v.(string); ok && strings.TrimSpace(s) == "" {
			// Blank falls through to due_on.
		} else {
			return dayField(row, "due_day")
		}
	}
	if raw, ok := row["due_on"].(string); ok && strings.TrimSpace(raw) != "" {
		if day, ok := parseDueDay(raw); ok {
			return strconv.Itoa(day), true
		}
	}
	return "", false
}

func parseDueDay(value string) (int, bool) {
	for _, layout := range []string{"2006-01-02", time.RFC3339, "2006-01-02 15:04:05", "2006/01/02"} {
		if t, err := time.Parse(layout, strings.TrimSpace(value)); err == nil {
			return t.Day(), true
		}
	}
	return 0, false
}
