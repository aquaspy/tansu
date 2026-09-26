package calendar

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aquasp/kuracalendar/internal/store"
)

// ImportCap mirrors CalendarImporter::CAP (per section).
const ImportCap = 500

// Imported is one parsed import file: event + birthday inputs ready for
// the store. Invalid rows are dropped by the caller (only valid rows are
// saved and counted), mirroring CalendarImporter.
type Imported struct {
	Events    []store.EventInput
	Birthdays []store.BirthdayInput
}

// ParseImport mirrors CalendarImporter.call: the payload must be an object
// with events/birthdays arrays (or the TansuCalendar app marker). Each
// section is capped at ImportCap; non-object rows are skipped.
func ParseImport(data []byte) (Imported, error) {
	var payload any
	if err := json.Unmarshal(data, &payload); err != nil {
		return Imported{}, err
	}
	obj, ok := payload.(map[string]any)
	if !ok {
		return Imported{}, errors.New("not an object")
	}
	events := asRows(obj["events"])
	birthdays := asRows(obj["birthdays"])
	// "KuraCalendar" is the pre-rebrand marker; old exports keep working.
	if app := obj["app"]; len(events) == 0 && len(birthdays) == 0 &&
		app != "TansuCalendar" && app != "KuraCalendar" {
		return Imported{}, errors.New("not a TansuCalendar export")
	}
	var out Imported
	for _, row := range events {
		if len(out.Events) >= ImportCap {
			break
		}
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		startsOn := stringField(m, "starts_on")
		endsOn := stringField(m, "ends_on")
		if endsOn == "" {
			endsOn = startsOn
		}
		allDay := true
		if v, present := m["all_day"]; present {
			allDay = truthy(v)
		}
		out.Events = append(out.Events, store.EventInput{
			Title:       stringField(m, "title"),
			Body:        stringField(m, "body"),
			AllDay:      allDay,
			StartsOn:    startsOn,
			EndsOn:      endsOn,
			StartsAt:    stringField(m, "starts_at"),
			EndsAt:      stringField(m, "ends_at"),
			Emoji:       stringField(m, "emoji"),
			Repeat:      stringField(m, "repeat"),
			RepeatUntil: stringField(m, "repeat_until"),
		})
	}
	for _, row := range birthdays {
		if len(out.Birthdays) >= ImportCap {
			break
		}
		m, ok := row.(map[string]any)
		if !ok {
			continue
		}
		out.Birthdays = append(out.Birthdays, store.BirthdayInput{
			Name:  stringField(m, "name"),
			Month: numberField(m, "month"),
			Day:   numberField(m, "day"),
			Year:  numberField(m, "year"),
			Body:  stringField(m, "body"),
			Emoji: stringField(m, "emoji"),
		})
	}
	return out, nil
}

// asRows mirrors Array(payload[...]): nil is empty, a list is kept, a
// single value is wrapped.
func asRows(v any) []any {
	switch t := v.(type) {
	case nil:
		return nil
	case []any:
		return t
	default:
		return []any{t}
	}
}

// truthy mirrors CalendarImporter.truthy?.
func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case bool:
		return t
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "0", "false", "no", "off":
			return false
		}
		return true
	case float64:
		return t != 0
	default:
		return true
	}
}

func stringField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// numberField renders a JSON number (or numeric string) for the birthday
// input; nil and garbage become "".
func numberField(m map[string]any, key string) string {
	switch v := m[key].(type) {
	case nil:
		return ""
	case float64:
		return strings.TrimSuffix(fmt.Sprintf("%v", v), ".0")
	case string:
		return strings.TrimSpace(v)
	case bool:
		return ""
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// Export (byte-compatible round-trip with ParseImport and the Rails export)
// ---------------------------------------------------------------------------

// ExportEvent mirrors Event#as_export (key order included). The trailing
// keys are Go-only additions; old exports without them still import.
type ExportEvent struct {
	Title       string  `json:"title"`
	Body        string  `json:"body"`
	AllDay      bool    `json:"all_day"`
	StartsOn    string  `json:"starts_on"`
	EndsOn      string  `json:"ends_on"`
	StartsAt    *string `json:"starts_at"`
	EndsAt      *string `json:"ends_at"`
	Emoji       string  `json:"emoji"`
	Repeat      string  `json:"repeat"`
	RepeatUntil *string `json:"repeat_until"`
}

// ExportBirthday mirrors Birthday#as_export (key order included).
type ExportBirthday struct {
	Name  string `json:"name"`
	Month int    `json:"month"`
	Day   int    `json:"day"`
	Year  *int   `json:"year"`
	Body  string `json:"body"`
	Emoji string `json:"emoji"`
}

// ExportPayload mirrors CalendarController#export.
type ExportPayload struct {
	App        string           `json:"app"`
	ExportedAt string           `json:"exported_at"`
	Events     []ExportEvent    `json:"events"`
	Birthdays  []ExportBirthday `json:"birthdays"`
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	v := s
	return &v
}

// BuildExport assembles the export payload in Rails ordering
// (events by starts_on/id, birthdays by month/day/id).
func BuildExport(st *store.Store, userID int64) (ExportPayload, error) {
	events, err := st.EventsForExport(userID)
	if err != nil {
		return ExportPayload{}, err
	}
	birthdays, err := st.ListBirthdays(userID)
	if err != nil {
		return ExportPayload{}, err
	}
	payload := ExportPayload{
		App:        "TansuCalendar",
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Events:     []ExportEvent{},
		Birthdays:  []ExportBirthday{},
	}
	for _, e := range events {
		payload.Events = append(payload.Events, ExportEvent{
			Title: e.Title, Body: e.Body, AllDay: e.AllDay,
			StartsOn: e.StartsOn, EndsOn: e.EndsOn,
			StartsAt: strPtr(e.StartsAt), EndsAt: strPtr(e.EndsAt),
			Emoji: e.Emoji, Repeat: e.Repeat, RepeatUntil: strPtr(e.RepeatUntil),
		})
	}
	for _, b := range birthdays {
		var year *int
		if b.Year != 0 {
			y := b.Year
			year = &y
		}
		payload.Birthdays = append(payload.Birthdays, ExportBirthday{
			Name: b.Name, Month: b.Month, Day: b.Day, Year: year, Body: b.Body,
			Emoji: b.Emoji,
		})
	}
	return payload, nil
}

// MarshalExport renders the payload like JSON.pretty_generate.
func MarshalExport(p ExportPayload) ([]byte, error) {
	return json.MarshalIndent(p, "", "  ")
}
