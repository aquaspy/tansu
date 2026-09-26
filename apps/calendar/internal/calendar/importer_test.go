package calendar

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aquasp/kuracalendar/internal/store"
)

func TestParseImportRoundTrip(t *testing.T) {
	st := openTest(t)
	u, _ := st.CreateUser("ada@example.com", "digest")
	if _, errs, err := st.CreateEvent(u.ID, store.EventInput{
		Title: "Call", AllDay: false, StartsOn: "2026-09-26", EndsOn: "2026-09-26",
		StartsAt: "14:30", EndsAt: "15:00", Body: "bring notes",
	}); err != nil || len(errs) > 0 {
		t.Fatalf("event: %v %+v", err, errs)
	}
	if _, errs, err := st.CreateEvent(u.ID, store.EventInput{
		Title: "Park", AllDay: true, StartsOn: "2026-08-25",
	}); err != nil || len(errs) > 0 {
		t.Fatalf("event: %v %+v", err, errs)
	}
	if _, errs, err := st.CreateBirthday(u.ID, store.BirthdayInput{
		Name: "Eben", Month: "8", Day: "11", Year: "1990",
	}); err != nil || len(errs) > 0 {
		t.Fatalf("birthday: %v %+v", err, errs)
	}

	payload, err := BuildExport(st, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := MarshalExport(payload)
	if err != nil {
		t.Fatal(err)
	}

	// The export must look like the Rails one (keys + pretty shape).
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["app"] != "TansuCalendar" {
		t.Fatalf("app = %v", raw["app"])
	}
	events := raw["events"].([]any)
	if len(events) != 2 {
		t.Fatalf("events = %d", len(events))
	}
	first := events[0].(map[string]any)
	if first["title"] != "Park" || first["all_day"] != true || first["starts_at"] != nil {
		t.Fatalf("first event = %+v", first)
	}
	second := events[1].(map[string]any)
	if second["starts_at"] != "14:30" || second["ends_at"] != "15:00" {
		t.Fatalf("timed event = %+v", second)
	}
	birthdays := raw["birthdays"].([]any)
	if len(birthdays) != 1 || birthdays[0].(map[string]any)["year"] != float64(1990) {
		t.Fatalf("birthdays = %+v", birthdays)
	}

	// Import the bytes into another user: same rows back.
	other, _ := st.CreateUser("lin@example.com", "digest")
	imported, err := ParseImport(data)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, in := range imported.Events {
		if _, errs, err := st.CreateEvent(other.ID, in); err == nil && len(errs) == 0 {
			count++
		}
	}
	for _, in := range imported.Birthdays {
		if _, errs, err := st.CreateBirthday(other.ID, in); err == nil && len(errs) == 0 {
			count++
		}
	}
	if count != 3 {
		t.Fatalf("imported = %d", count)
	}
	back, err := st.EventsInRange(other.ID, "2026-01-01", "2026-12-31")
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 2 || back[1].StartsAt != "14:30" || back[1].Body != "bring notes" {
		t.Fatalf("round trip = %+v", back)
	}
}

func TestParseImportRoundTripNewFields(t *testing.T) {
	st := openTest(t)
	u, _ := st.CreateUser("ada@example.com", "digest")
	if _, errs, err := st.CreateEvent(u.ID, store.EventInput{
		Title: "Gym", AllDay: true, StartsOn: "2026-09-07", EndsOn: "2026-09-07",
		Emoji: "🏋️", Repeat: "weekly", RepeatUntil: "2026-12-07",
	}); err != nil || len(errs) > 0 {
		t.Fatalf("event: %v %+v", err, errs)
	}
	if _, errs, err := st.CreateBirthday(u.ID, store.BirthdayInput{
		Name: "Eben", Month: "8", Day: "11", Emoji: "🎂",
	}); err != nil || len(errs) > 0 {
		t.Fatalf("birthday: %v %+v", err, errs)
	}
	payload, err := BuildExport(st, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := MarshalExport(payload)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	first := raw["events"].([]any)[0].(map[string]any)
	if first["emoji"] != "🏋️" || first["repeat"] != "weekly" || first["repeat_until"] != "2026-12-07" {
		t.Fatalf("event keys = %+v", first)
	}
	// Reimport into another user: new fields survive the round trip.
	other, _ := st.CreateUser("lin@example.com", "digest")
	imported, err := ParseImport(data)
	if err != nil {
		t.Fatal(err)
	}
	e, errs, err := st.CreateEvent(other.ID, imported.Events[0])
	if err != nil || len(errs) > 0 {
		t.Fatalf("reimport event: %v %+v", err, errs)
	}
	if e.Emoji != "🏋️" || e.Repeat != "weekly" || e.RepeatUntil != "2026-12-07" {
		t.Fatalf("reimported: %+v", e)
	}
	b, errs, err := st.CreateBirthday(other.ID, imported.Birthdays[0])
	if err != nil || len(errs) > 0 {
		t.Fatalf("reimport birthday: %v %+v", err, errs)
	}
	if b.Emoji != "🎂" {
		t.Fatalf("reimported birthday: %+v", b)
	}
}

func TestParseImportRailsBytes(t *testing.T) {
	// Bytes shaped exactly like a Rails JSON.pretty_generate export.
	data := `{
  "app": "KuraCalendar",
  "exported_at": "2026-09-19T10:00:00Z",
  "events": [
    {
      "title": "Dentist",
      "body": "",
      "all_day": true,
      "starts_on": "2026-09-25",
      "ends_on": "2026-09-25",
      "starts_at": null,
      "ends_at": null
    }
  ],
  "birthdays": [
    {
      "name": "Eben",
      "month": 8,
      "day": 11,
      "year": null,
      "body": ""
    }
  ]
}`
	imported, err := ParseImport([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(imported.Events) != 1 || len(imported.Birthdays) != 1 {
		t.Fatalf("parsed = %+v", imported)
	}
	if imported.Events[0].Title != "Dentist" || !imported.Events[0].AllDay {
		t.Fatalf("event = %+v", imported.Events[0])
	}
	if imported.Birthdays[0].Name != "Eben" || imported.Birthdays[0].Year != "" {
		t.Fatalf("birthday = %+v", imported.Birthdays[0])
	}
}

func TestParseImportRejects(t *testing.T) {
	for name, data := range map[string]string{
		"garbage":   "not json",
		"array":     "[]",
		"unmarked":  `{"events": [], "birthdays": []}`,
		"other app": `{"app": "KuraNotes", "events": [], "birthdays": []}`,
		"empty":     "",
	} {
		if _, err := ParseImport([]byte(data)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	// The app marker alone is a valid (empty) import, new or pre-rebrand.
	for _, marker := range []string{"TansuCalendar", "KuraCalendar"} {
		if _, err := ParseImport([]byte(`{"app": "` + marker + `"}`)); err != nil {
			t.Errorf("marker-only %s: %v", marker, err)
		}
	}
}

func TestParseImportSkipsBadRowsAndCaps(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"app": "TansuCalendar", "events": [`)
	for i := 0; i < ImportCap+50; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"title": "E", "starts_on": "2026-01-01"}`)
	}
	b.WriteString(`], "birthdays": ["nope", 42, {"name": "Ok", "month": 1, "day": 2}]}`)
	imported, err := ParseImport([]byte(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if len(imported.Events) != ImportCap {
		t.Fatalf("events = %d", len(imported.Events))
	}
	if len(imported.Birthdays) != 1 || imported.Birthdays[0].Name != "Ok" {
		t.Fatalf("birthdays = %+v", imported.Birthdays)
	}
}

func TestParseImportAllDayTruthy(t *testing.T) {
	data := `{"events": [
		{"title": "A", "starts_on": "2026-01-01"},
		{"title": "B", "starts_on": "2026-01-01", "all_day": false},
		{"title": "C", "starts_on": "2026-01-01", "all_day": "0"},
		{"title": "D", "starts_on": "2026-01-01", "all_day": "off"},
		{"title": "E", "starts_on": "2026-01-01", "all_day": 0}
	], "birthdays": []}`
	imported, err := ParseImport([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	want := []bool{true, false, false, false, false}
	for i, w := range want {
		if imported.Events[i].AllDay != w {
			t.Errorf("row %d all_day = %v, want %v", i, imported.Events[i].AllDay, w)
		}
	}
}
