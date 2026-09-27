package suite

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestShapeListDropsBodies(t *testing.T) {
	notes := map[string]any{"notes": []any{}}
	for i := 0; i < 25; i++ {
		notes["notes"] = append(notes["notes"].([]any), map[string]any{
			"id": i, "title": "T", "preview": "P", "folder": "f", "body": "SECRET BODY",
		})
	}
	raw, _ := json.Marshal(notes)
	out, err := ShapeList("notes", raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "SECRET") {
		t.Fatalf("body leaked: %s", out)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	rows, _ := doc["notes"].([]any)
	if len(rows) != 20 || doc["truncated"] != true {
		t.Fatalf("rows=%d truncated=%v", len(rows), doc["truncated"])
	}
}

func TestShapeOneClips(t *testing.T) {
	body := strings.Repeat("á", readRunes+10)
	raw, _ := json.Marshal(map[string]any{"note": map[string]any{"id": 1, "body": body}})
	out, err := ShapeOne("notes", raw)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.Unmarshal(out, &doc)
	note := doc["note"].(map[string]any)
	if note["truncated"] != true {
		t.Fatal("expected truncated")
	}
	if n := len([]rune(note["body"].(string))); n != readRunes {
		t.Fatalf("runes %d", n)
	}
}
