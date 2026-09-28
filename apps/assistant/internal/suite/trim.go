package suite

import (
	"encoding/json"
	"unicode/utf8"
)

const (
	listLimit = 20
	listRunes = 6000
	readRunes = 8000
)

func clip(s string, n int) (string, bool) {
	if utf8.RuneCountInString(s) <= n {
		return s, false
	}
	r := []rune(s)
	return string(r[:n]), true
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func str(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return s
}

// ShapeList trims an index payload down to ids and short fields.
// kind is the JSON array key: notes, people, events, expenses,
// subscriptions, payment_days, folders, accounts, mail_folders, or messages.
func ShapeList(kind string, payload []byte) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(payload, &doc); err != nil {
		return nil, err
	}
	key := kind
	raw, _ := doc[key].([]any)
	if len(raw) > listLimit {
		raw = raw[:listLimit]
	}
	out := make([]any, 0, len(raw))
	truncated := len(raw) < countSlice(doc[key])
	for _, item := range raw {
		m := asMap(item)
		if m == nil {
			continue
		}
		switch kind {
		case "notes":
			out = append(out, map[string]any{
				"id": m["id"], "title": str(m, "title"), "preview": str(m, "preview"), "folder": str(m, "folder"),
			})
		case "people":
			notes, cut := clip(str(m, "notes"), 160)
			if cut {
				truncated = true
			}
			out = append(out, map[string]any{
				"id": m["id"], "name": str(m, "name"), "nickname": str(m, "nickname"),
				"relationship": str(m, "relationship"), "notes": notes,
			})
		case "events":
			out = append(out, map[string]any{
				"id": m["id"], "title": str(m, "title"), "starts_on": str(m, "starts_on"),
				"ends_on": str(m, "ends_on"), "all_day": m["all_day"], "starts_at": m["starts_at"],
				"ends_at": m["ends_at"], "emoji": str(m, "emoji"), "occurrence_on": str(m, "occurrence_on"),
				"repeat": str(m, "repeat"), "repeat_until": m["repeat_until"],
			})
		case "expenses":
			notes, cut := clip(str(m, "notes"), 160)
			if cut {
				truncated = true
			}
			out = append(out, map[string]any{
				"id": m["id"], "title": str(m, "title"), "category": str(m, "category"),
				"spent_on": str(m, "spent_on"), "amount_cents": m["amount_cents"], "currency": str(m, "currency"),
				"notes": notes,
			})
		case "subscriptions":
			notes, cut := clip(str(m, "notes"), 160)
			if cut {
				truncated = true
			}
			out = append(out, map[string]any{
				"id": m["id"], "title": str(m, "title"), "amount_cents": m["amount_cents"],
				"currency": str(m, "currency"), "interval": str(m, "interval"),
				"active": m["active"], "notes": notes,
			})
		case "payment_days":
			notes, cut := clip(str(m, "notes"), 160)
			if cut {
				truncated = true
			}
			out = append(out, map[string]any{
				"id": m["id"], "title": str(m, "title"), "due_day": m["due_day"],
				"active": m["active"], "notes": notes,
			})
		case "folders":
			out = append(out, map[string]any{"name": str(m, "name"), "count": m["count"]})
		case "accounts":
			out = append(out, map[string]any{
				"id": m["id"], "display_name": str(m, "display_name"), "from_address": str(m, "from_address"),
				"username": str(m, "username"), "last_ok": m["last_ok"], "last_error": str(m, "last_error"),
			})
		case "mail_folders":
			out = append(out, map[string]any{"name": str(m, "name"), "special": str(m, "special")})
		case "messages":
			out = append(out, map[string]any{
				"uid": m["uid"], "from": str(m, "from"), "subject": str(m, "subject"),
				"date": str(m, "date"), "seen": m["seen"],
			})
		}
	}
	body, err := json.Marshal(map[string]any{key: out, "truncated": truncated})
	if err != nil {
		return nil, err
	}
	if utf8.RuneCount(body) > listRunes {
		for _, item := range out {
			if m, ok := item.(map[string]any); ok {
				delete(m, "preview")
				delete(m, "notes")
			}
		}
		body, _ = json.Marshal(map[string]any{key: out, "truncated": true})
	}
	return body, nil
}

func countSlice(v any) int {
	s, _ := v.([]any)
	return len(s)
}

// ShapeOne clips a single record's long text fields.
func ShapeOne(kind string, payload []byte) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(payload, &doc); err != nil {
		return nil, err
	}
	key := map[string]string{
		"notes": "note", "note": "note",
		"people": "person", "person": "person",
		"events": "event", "event": "event",
		"expenses": "expense", "expense": "expense",
		"subscriptions": "subscription", "subscription": "subscription",
		"payment_days": "payment_day", "payment_day": "payment_day",
		"message": "message",
	}[kind]
	m := asMap(doc[key])
	if m == nil {
		return payload, nil
	}
	truncated := false
	for _, field := range []string{"body", "notes", "text"} {
		if s, ok := m[field].(string); ok {
			clipped, cut := clip(s, readRunes)
			m[field] = clipped
			truncated = truncated || cut
		}
	}
	if truncated {
		m["truncated"] = true
	}
	doc[key] = m
	return json.Marshal(doc)
}
