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
// kind is notes, people, events, or expenses.
func ShapeList(kind string, payload []byte) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(payload, &doc); err != nil {
		return nil, err
	}
	key := map[string]string{"notes": "notes", "people": "people", "events": "events", "expenses": "expenses"}[kind]
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
			})
		case "expenses":
			out = append(out, map[string]any{
				"id": m["id"], "title": str(m, "title"), "category": str(m, "category"),
				"spent_on": str(m, "spent_on"), "amount_cents": m["amount_cents"], "currency": str(m, "currency"),
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
	key := map[string]string{"notes": "note", "people": "person", "events": "event", "expenses": "expense"}[kind]
	m := asMap(doc[key])
	if m == nil {
		return payload, nil
	}
	truncated := false
	for _, field := range []string{"body", "notes"} {
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
