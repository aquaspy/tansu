package suite

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// flattenRecord lifts a nested {"expense": {...}} (or event, note, …) up to
// the top level. Top-level keys win, except an empty top-level value, which
// does not wipe a nested one. Models wrap the payload the same way they
// wrapped person before people_create learned to flatten.
func flattenRecord(args map[string]any, key string) map[string]any {
	if args == nil {
		return map[string]any{}
	}
	inner, ok := args[key].(map[string]any)
	if !ok {
		return args
	}
	out := make(map[string]any, len(inner)+len(args))
	for k, v := range inner {
		out[k] = v
	}
	for k, v := range args {
		if k == key || v == nil || v == "" {
			continue
		}
		out[k] = v
	}
	return out
}

func unwrapArgs(name string, args map[string]any) map[string]any {
	if args == nil {
		args = map[string]any{}
	}
	switch {
	case strings.Contains(name, "subscription"):
		return flattenRecord(args, "subscription")
	case strings.Contains(name, "payment_day"):
		return flattenRecord(args, "payment_day")
	case strings.HasPrefix(name, "notes_folder"):
		return args
	case strings.HasPrefix(name, "notes"):
		return flattenRecord(args, "note")
	case strings.HasPrefix(name, "calendar"):
		return flattenRecord(args, "event")
	case strings.HasPrefix(name, "people"):
		return flattenRecord(args, "person")
	case strings.HasPrefix(name, "spend"):
		return flattenRecord(args, "expense")
	}
	return args
}

func nonemptyString(args map[string]any, key string) (string, bool) {
	v, ok := args[key]
	if !ok || v == nil {
		return "", false
	}
	s := strings.TrimSpace(scalarString(v))
	if s == "" {
		return "", false
	}
	return s, true
}

func putString(args, out map[string]any, key string) {
	if s, ok := nonemptyString(args, key); ok {
		out[key] = s
	}
}

func boolArg(v any) (bool, bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "1", "t", "true", "y", "yes", "on":
			return true, true
		case "0", "f", "false", "n", "no", "off":
			return false, true
		}
	case float64:
		return t != 0, true
	case int:
		return t != 0, true
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i != 0, true
		}
	}
	return false, false
}

func putBool(args, out map[string]any, key string) {
	v, ok := args[key]
	if !ok || v == nil || v == "" {
		return
	}
	if b, ok := boolArg(v); ok {
		out[key] = b
	}
}

// putDay forwards a day-of-month (or month-of-year) when the model sent one.
// Blank means "leave it unchanged", matching the other partial writes.
func putDay(args, out map[string]any, key string) {
	v, ok := args[key]
	if !ok || v == nil || v == "" {
		return
	}
	if s, isStr := v.(string); isStr {
		s = strings.TrimSpace(s)
		if _, parsed := leadingInt(s); !parsed {
			out[key] = s
			return
		}
	}
	out[key] = intField(args, key)
}

func applyMoney(args, out map[string]any) {
	if v, ok := args["amount_cents"]; ok && v != nil && v != "" {
		switch n := v.(type) {
		case float64:
			if n == float64(int64(n)) {
				out["amount_cents"] = int64(n)
			} else {
				out["amount"] = strconv.FormatFloat(n, 'f', -1, 64)
			}
		case int:
			out["amount_cents"] = n
		case int64:
			out["amount_cents"] = n
		case json.Number:
			f, err := n.Float64()
			if err != nil {
				break
			}
			if f == float64(int64(f)) {
				out["amount_cents"] = int64(f)
			} else if s := strings.TrimSpace(n.String()); s != "" {
				out["amount"] = s
			}
		case string:
			s := strings.TrimSpace(n)
			if s == "" {
				break
			}
			if i, err := strconv.ParseInt(s, 10, 64); err == nil {
				out["amount_cents"] = i
			} else {
				out["amount"] = s
			}
		}
	}
	if v, ok := args["amount"]; ok && v != nil {
		if s := moneyString(v); s != "" {
			out["amount"] = s
		}
	}
}

func moneyString(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case json.Number:
		return strings.TrimSpace(t.String())
	default:
		return ""
	}
}

func normalizeCurrency(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "brl", "r$", "real", "reais":
		return "BRL"
	case "usd", "us$", "$", "dollar", "dollars", "dólar", "dolar", "dolares", "dólares":
		return "USD"
	case "eur", "€", "euro", "euros":
		return "EUR"
	default:
		return strings.ToUpper(strings.TrimSpace(s))
	}
}

func normalizeCategory(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "food", "comida", "alimentação", "alimentacao", "restaurant", "restaurante", "groceries", "mercado", "dining":
		return "food"
	case "transport", "transporte", "transit":
		return "transport"
	case "home", "casa", "moradia", "house":
		return "home"
	case "health", "saúde", "saude", "medical":
		return "health"
	case "leisure", "lazer", "entertainment":
		return "leisure"
	case "other", "outro", "outros", "misc", "miscellaneous":
		return "other"
	default:
		return strings.ToLower(strings.TrimSpace(s))
	}
}

func normalizeInterval(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "monthly", "month", "mensal", "mensalmente", "every month":
		return "monthly"
	case "yearly", "year", "anual", "annually", "anually", "anualmente", "every year":
		return "yearly"
	default:
		return strings.ToLower(strings.TrimSpace(s))
	}
}

func normalizeRepeat(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "daily", "day", "diario", "diário", "every day", "everyday":
		return "daily"
	case "weekly", "week", "semanal", "every week":
		return "weekly"
	case "monthly", "month", "mensal", "every month":
		return "monthly"
	case "yearly", "year", "anual", "annually", "anually", "anualmente", "every year":
		return "yearly"
	case "none", "no", "never", "não", "nao", "once", "one-shot", "oneshot":
		return "none"
	default:
		return strings.ToLower(strings.TrimSpace(s))
	}
}

// normalizeISODate accepts YYYY-MM-DD, day/month/year (19/09/2026), and
// prose such as "19 September 2026". Ambiguous numeric dates are day/month
// because the suite clock is America/Sao_Paulo. Year is required.
func normalizeISODate(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	if _, ok := parseYMD(s); ok {
		return s, true
	}
	norm := strings.NewReplacer("/", "-", ".", "-").Replace(s)
	parts := strings.Split(norm, "-")
	if len(parts) == 3 {
		nums := make([]int, 3)
		numeric := true
		for i, p := range parts {
			n, err := strconv.Atoi(strings.TrimSpace(p))
			if err != nil {
				numeric = false
				break
			}
			nums[i] = n
		}
		if numeric {
			y, m, d := 0, 0, 0
			switch {
			case nums[0] >= 1900 && nums[0] <= 2100:
				y, m, d = nums[0], nums[1], nums[2]
			case nums[2] >= 1900 && nums[2] <= 2100 && nums[0] > 12:
				y, m, d = nums[2], nums[1], nums[0]
			case nums[2] >= 1900 && nums[2] <= 2100 && nums[1] > 12:
				y, m, d = nums[2], nums[0], nums[1]
			case nums[2] >= 1900 && nums[2] <= 2100:
				y, m, d = nums[2], nums[1], nums[0]
			}
			if y != 0 {
				if iso, ok := ymd(y, m, d); ok {
					return iso, true
				}
			}
		}
	}
	if m, d, y, ok := proseDate(s); ok && y != 0 {
		if iso, ok := ymd(y, m, d); ok {
			return iso, true
		}
	}
	return "", false
}

func parseYMD(s string) (string, bool) {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return "", false
	}
	y, errY := strconv.Atoi(s[0:4])
	m, errM := strconv.Atoi(s[5:7])
	d, errD := strconv.Atoi(s[8:10])
	if errY != nil || errM != nil || errD != nil {
		return "", false
	}
	return ymd(y, m, d)
}

func ymd(y, m, d int) (string, bool) {
	if y < 1 || m < 1 || m > 12 || d < 1 {
		return "", false
	}
	iso := fmt.Sprintf("%04d-%02d-%02d", y, m, d)
	t, err := time.Parse("2006-01-02", iso)
	if err != nil || t.Year() != y || int(t.Month()) != m || t.Day() != d {
		return "", false
	}
	return iso, true
}

func normalizeDateOrRaw(s string) string {
	if iso, ok := normalizeISODate(s); ok {
		return iso
	}
	return strings.TrimSpace(s)
}

func normalizeMonth(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if iso, ok := normalizeISODate(s); ok {
		return iso[:7]
	}
	if !strings.ContainsAny(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		if iso, ok := normalizeISODate(s + "-01"); ok {
			return iso[:7]
		}
	}
	if m, _, y, ok := proseDate("1 " + s); ok && y != 0 && m != 0 {
		return fmt.Sprintf("%04d-%02d", y, m)
	}
	if m, _, y, ok := proseDate(s + " 1"); ok && y != 0 && m != 0 {
		return fmt.Sprintf("%04d-%02d", y, m)
	}
	return s
}

var (
	clockHour = regexp.MustCompile(`^(\d{1,2})h(\d{2})?$`)
	clockHM   = regexp.MustCompile(`^(\d{1,2}):(\d{2})(?::\d{2})?$`)
	clockAP   = regexp.MustCompile(`^(\d{1,2})(?::(\d{2}))?\s*([ap]m)$`)
)

// normalizeClock accepts HH:MM, HH:MM:SS, 2:30 PM, 2pm, and 14h30.
func normalizeClock(s string) (string, bool) {
	raw := strings.ToLower(strings.TrimSpace(s))
	raw = strings.ReplaceAll(raw, ".", "")
	if raw == "" {
		return "", false
	}
	if m := clockHour.FindStringSubmatch(raw); m != nil {
		return clockParts(m[1], m[2], false, false)
	}
	if m := clockAP.FindStringSubmatch(raw); m != nil {
		return clockParts(m[1], m[2], m[3] == "pm", true)
	}
	if m := clockHM.FindStringSubmatch(raw); m != nil {
		return clockParts(m[1], m[2], false, false)
	}
	return "", false
}

func clockParts(hs, ms string, pm, ampm bool) (string, bool) {
	h, err := strconv.Atoi(hs)
	if err != nil {
		return "", false
	}
	min := 0
	if ms != "" {
		min, err = strconv.Atoi(ms)
		if err != nil || min > 59 {
			return "", false
		}
	}
	if ampm {
		if h < 1 || h > 12 {
			return "", false
		}
		if h == 12 {
			h = 0
		}
		if pm {
			h += 12
		}
	}
	if h > 23 {
		return "", false
	}
	return fmt.Sprintf("%02d:%02d", h, min), true
}

func putDate(args, out map[string]any, key string) {
	s, ok := nonemptyString(args, key)
	if !ok {
		return
	}
	out[key] = normalizeDateOrRaw(s)
}

func putClock(args, out map[string]any, key string) {
	s, ok := nonemptyString(args, key)
	if !ok {
		return
	}
	if clock, ok := normalizeClock(s); ok {
		out[key] = clock
		return
	}
	out[key] = s
}

func expenseBody(args map[string]any) map[string]any {
	args = flattenRecord(args, "expense")
	out := map[string]any{}
	putString(args, out, "title")
	applyMoney(args, out)
	if s, ok := nonemptyString(args, "currency"); ok {
		out["currency"] = normalizeCurrency(s)
	}
	putDate(args, out, "spent_on")
	if s, ok := nonemptyString(args, "category"); ok {
		out["category"] = normalizeCategory(s)
	}
	if s, ok := nonemptyString(args, "notes"); ok {
		out["notes"] = joinExtra(s, args, "tags", "splits")
	} else if extra := joinExtra("", args, "tags", "splits"); extra != "" {
		out["notes"] = extra
	}
	return out
}

func subscriptionBody(args map[string]any) map[string]any {
	args = flattenRecord(args, "subscription")
	out := map[string]any{}
	putString(args, out, "title")
	applyMoney(args, out)
	if s, ok := nonemptyString(args, "currency"); ok {
		out["currency"] = normalizeCurrency(s)
	}
	if s, ok := nonemptyString(args, "interval"); ok {
		out["interval"] = normalizeInterval(s)
	}
	putBool(args, out, "active")
	if s, ok := nonemptyString(args, "notes"); ok {
		out["notes"] = joinExtra(s, args, "tags")
	} else if extra := joinExtra("", args, "tags"); extra != "" {
		out["notes"] = extra
	}
	return out
}

func paymentDayBody(args map[string]any) map[string]any {
	args = flattenRecord(args, "payment_day")
	out := map[string]any{}
	putString(args, out, "title")
	putDay(args, out, "due_day")
	putBool(args, out, "active")
	putString(args, out, "notes")
	return out
}

func eventBody(args map[string]any) map[string]any {
	args = flattenRecord(args, "event")
	out := map[string]any{}
	putString(args, out, "title")
	putString(args, out, "emoji")
	if s, ok := nonemptyString(args, "repeat"); ok {
		out["repeat"] = normalizeRepeat(s)
	}
	putDate(args, out, "starts_on")
	putDate(args, out, "ends_on")
	putDate(args, out, "repeat_until")

	allDay, hasAllDay := false, false
	if v, ok := args["all_day"]; ok && v != nil && v != "" {
		if b, ok := boolArg(v); ok {
			allDay, hasAllDay = b, true
		}
	}
	if !hasAllDay || !allDay {
		putClock(args, out, "starts_at")
		putClock(args, out, "ends_at")
	}
	if hasAllDay {
		out["all_day"] = allDay
	} else if out["starts_at"] != nil || out["ends_at"] != nil {
		// The API defaults all_day to true and then drops the times.
		out["all_day"] = false
	}

	body, _ := nonemptyString(args, "body")
	body = joinExtra(body, args, "location", "attendees")
	if body != "" {
		out["body"] = body
	}
	return out
}

func noteBody(args map[string]any) map[string]any {
	args = flattenRecord(args, "note")
	out := map[string]any{}
	body, _ := args["body"].(string)
	title := strings.TrimSpace(strArg(args, "title"))
	if title != "" || strings.TrimSpace(body) != "" {
		out["body"] = composeNoteBody(title, body)
	}
	if folder, ok := folderValue(args); ok {
		out["folder"] = folder
	}
	return out
}

func composeNoteBody(title, body string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return body
	}
	if strings.TrimSpace(body) == "" {
		return title
	}
	if strings.EqualFold(firstNonBlankLine(body), title) {
		return body
	}
	return title + "\n" + body
}

func firstNonBlankLine(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func folderValue(args map[string]any) (string, bool) {
	v, ok := args["folder"]
	if !ok || v == nil {
		return "", false
	}
	s := strings.TrimSpace(scalarString(v))
	if s == "" || strings.EqualFold(s, "inbox") {
		return "inbox", true
	}
	return s, true
}

// mergeNoteTitle rewrites the first line of an existing note when the model
// sends title without body, so a rename does not wipe the rest of the text.
func mergeNoteTitle(ctx context.Context, c *Client, args map[string]any) (map[string]any, error) {
	title := strings.TrimSpace(strArg(args, "title"))
	if title == "" {
		return args, nil
	}
	if _, ok := args["body"].(string); ok {
		return args, nil
	}
	id := idArg(args)
	if id == 0 || c == nil {
		return args, nil
	}
	status, raw, err := c.Call(ctx, http.MethodGet, "/api/v1/notes/"+strconv.FormatInt(id, 10), nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("status %d", status)
	}
	var doc map[string]any
	if json.Unmarshal(raw, &doc) != nil {
		return nil, fmt.Errorf("note")
	}
	note, _ := doc["note"].(map[string]any)
	existing, _ := note["body"].(string)
	copied := make(map[string]any, len(args)+1)
	for k, v := range args {
		copied[k] = v
	}
	copied["body"] = replaceNoteTitle(existing, title)
	return copied, nil
}

func replaceNoteTitle(body, title string) string {
	rest := body
	for {
		line, after, found := strings.Cut(rest, "\n")
		if strings.TrimSpace(line) == "" {
			if !found {
				return title
			}
			rest = after
			continue
		}
		if !found {
			return title
		}
		return title + "\n" + after
	}
}

func joinExtra(base string, args map[string]any, keys ...string) string {
	for _, key := range keys {
		extra := renderExtra(args[key])
		if extra == "" || strings.Contains(base, extra) {
			continue
		}
		line := key + ": " + extra
		if strings.TrimSpace(base) == "" {
			base = line
			continue
		}
		base += "\n" + line
	}
	return base
}

func renderExtra(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case []any:
		var bits []string
		for _, item := range t {
			if s := scalarString(item); s != "" {
				bits = append(bits, s)
				continue
			}
			if m, ok := item.(map[string]any); ok {
				if s := compactMap(m); s != "" {
					bits = append(bits, s)
				}
			}
		}
		return strings.Join(bits, ", ")
	default:
		return ""
	}
}

func compactMap(m map[string]any) string {
	var bits []string
	for _, key := range []string{"name", "title", "person", "amount", "value"} {
		if s := scalarString(m[key]); s != "" {
			bits = append(bits, s)
		}
	}
	return strings.Join(bits, " ")
}
