package handler

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/aquasp/kuracalendar/internal/store"
)

func newEventIn(title, from, to string) store.EventInput {
	return store.EventInput{Title: title, AllDay: true, StartsOn: from, EndsOn: to}
}

func TestAPIUnauthorized(t *testing.T) {
	f := newFlow(t, nil)
	if code, out := f.apiCall(http.MethodGet, "/api/v1/events", "", ""); code != http.StatusUnauthorized || out["error"] != "unauthorized" {
		t.Fatalf("no token: %d %+v", code, out)
	}
	if code, _ := f.apiCall(http.MethodGet, "/api/v1/events", "kura_nope", ""); code != http.StatusUnauthorized {
		t.Fatalf("bad token: %d", code)
	}
}

func TestAPIEventsRange(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	_, raw, _ := f.store.CreateToken(u.ID, "hermes")
	if _, errs, err := f.store.CreateEvent(u.ID, newEventIn("Dentist", "2026-09-25", "2026-09-25")); err != nil || len(errs) > 0 {
		t.Fatalf("seed: %v %+v", err, errs)
	}
	if _, errs, err := f.store.CreateEvent(u.ID, newEventIn("Far away", "2026-11-01", "2026-11-01")); err != nil || len(errs) > 0 {
		t.Fatalf("seed: %v %+v", err, errs)
	}
	code, out := f.apiCall(http.MethodGet, "/api/v1/events?from=2026-09-01&to=2026-09-30", raw, "")
	if code != http.StatusOK {
		t.Fatalf("index: %d %+v", code, out)
	}
	events := out["events"].([]any)
	if len(events) != 1 || events[0].(map[string]any)["title"] != "Dentist" {
		t.Fatalf("range: %+v", out)
	}
}

func TestAPIEventsExpandSeries(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	_, raw, _ := f.store.CreateToken(u.ID, "hermes")

	code, out := f.apiCall(http.MethodPost, "/api/v1/events", raw,
		`{"event":{"title":"Gym","starts_on":"2026-09-07","emoji":"🏋️","repeat":"weekly"}}`)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %+v", code, out)
	}
	created := out["event"].(map[string]any)
	if created["emoji"] != "🏋️" || created["repeat"] != "weekly" || created["occurrence_on"] != "2026-09-07" {
		t.Fatalf("shape: %+v", created)
	}

	code, out = f.apiCall(http.MethodGet, "/api/v1/events?from=2026-09-01&to=2026-09-30", raw, "")
	if code != http.StatusOK {
		t.Fatalf("index: %d %+v", code, out)
	}
	events := out["events"].([]any)
	if len(events) != 4 {
		t.Fatalf("occurrences = %d: %+v", len(events), out)
	}
	first := events[0].(map[string]any)
	last := events[3].(map[string]any)
	if first["occurrence_on"] != "2026-09-07" || first["starts_on"] != "2026-09-07" {
		t.Fatalf("first: %+v", first)
	}
	if last["occurrence_on"] != "2026-09-28" || last["starts_on"] != "2026-09-28" {
		t.Fatalf("last: %+v", last)
	}
	if first["id"] != last["id"] || first["emoji"] != "🏋️" || first["repeat"] != "weekly" {
		t.Fatalf("series identity: %+v %+v", first, last)
	}

	// Invalid repeat + overlong emoji reject with 422.
	code, out = f.apiCall(http.MethodPost, "/api/v1/events", raw,
		`{"event":{"title":"Bad","starts_on":"2026-09-07","repeat":"fortnightly"}}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("bad repeat: %d %+v", code, out)
	}
	_ = u
}

func TestAPIBirthdayEmoji(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	_, raw, _ := f.store.CreateToken(u.ID, "hermes")
	code, out := f.apiCall(http.MethodPost, "/api/v1/birthdays", raw,
		`{"birthday":{"name":"Ada","month":12,"day":10,"emoji":"🎂"}}`)
	if code != http.StatusUnprocessableEntity || out["error"] != "birthdays_retired" {
		t.Fatalf("create: %d %+v", code, out)
	}
	b, errs, err := f.store.CreateBirthday(u.ID, store.BirthdayInput{
		Name: "Ada", Month: "12", Day: "10", Emoji: "🎂",
	})
	if err != nil || len(errs) > 0 {
		t.Fatalf("seed: %v %+v", err, errs)
	}
	id := strconv.FormatInt(b.ID, 10)
	code, out = f.apiCall(http.MethodPatch, "/api/v1/birthdays/"+id, raw, `{"birthday":{"name":"Ada L."}}`)
	if code != http.StatusOK || out["birthday"].(map[string]any)["emoji"] != "🎂" {
		t.Fatalf("emoji kept on patch: %d %+v", code, out)
	}
	_ = u
}

func TestAPIEventCRUD(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	_, raw, _ := f.store.CreateToken(u.ID, "hermes")

	code, out := f.apiCall(http.MethodPost, "/api/v1/events", raw,
		`{"event":{"title":"Call","starts_on":"2026-09-26","all_day":false,"starts_at":"14:30","ends_at":"15:00"}}`)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %+v", code, out)
	}
	created := out["event"].(map[string]any)
	if created["title"] != "Call" || created["starts_at"] != "14:30" {
		t.Fatalf("shape: %+v", created)
	}
	id := strconv.FormatFloat(created["id"].(float64), 'f', 0, 64)

	code, out = f.apiCall(http.MethodGet, "/api/v1/events/"+id, raw, "")
	if code != http.StatusOK || out["event"].(map[string]any)["title"] != "Call" {
		t.Fatalf("show: %d %+v", code, out)
	}

	code, out = f.apiCall(http.MethodPatch, "/api/v1/events/"+id, raw, `{"event":{"title":"Call rescheduled"}}`)
	if code != http.StatusOK || out["event"].(map[string]any)["title"] != "Call rescheduled" {
		t.Fatalf("patch: %d %+v", code, out)
	}
	// Partial update keeps the times.
	if out["event"].(map[string]any)["starts_at"] != "14:30" {
		t.Fatalf("merge: %+v", out["event"])
	}

	code, _ = f.apiCall(http.MethodDelete, "/api/v1/events/"+id, raw, "")
	if code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	code, out = f.apiCall(http.MethodGet, "/api/v1/events/"+id, raw, "")
	if code != http.StatusNotFound || out["error"] != "not_found" {
		t.Fatalf("deleted show: %d %+v", code, out)
	}
	_ = u
}

func TestAPIEventFlatParams(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	_, raw, _ := f.store.CreateToken(u.ID, "hermes")
	code, out := f.apiCall(http.MethodPost, "/api/v1/events", raw, `{"title":"Flat","starts_on":"2026-09-26"}`)
	if code != http.StatusCreated || out["event"].(map[string]any)["title"] != "Flat" {
		t.Fatalf("flat: %d %+v", code, out)
	}
	_ = u
}

func TestAPIEventInvalid(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	_, raw, _ := f.store.CreateToken(u.ID, "hermes")
	code, out := f.apiCall(http.MethodPost, "/api/v1/events", raw, `{"event":{"title":""}}`)
	if code != http.StatusUnprocessableEntity || len(out["errors"].([]any)) == 0 {
		t.Fatalf("invalid: %d %+v", code, out)
	}
	code, out = f.apiCall(http.MethodGet, "/api/v1/events?from=not-a-date", raw, "")
	if code != http.StatusUnprocessableEntity || len(out["errors"].([]any)) == 0 {
		t.Fatalf("bad filter: %d %+v", code, out)
	}
	_ = u
}

func TestAPIBirthdaysCRUD(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	_, raw, _ := f.store.CreateToken(u.ID, "hermes")

	code, out := f.apiCall(http.MethodPost, "/api/v1/birthdays", raw,
		`{"birthday":{"name":"Eben","month":8,"day":11,"year":1990}}`)
	if code != http.StatusUnprocessableEntity || out["error"] != "birthdays_retired" {
		t.Fatalf("create: %d %+v", code, out)
	}
	b, errs, err := f.store.UpsertSyncedBirthday(u.ID, "people:7", store.BirthdayInput{
		Name: "Eben", Month: "8", Day: "11", Year: "1990",
	})
	if err != nil || len(errs) > 0 {
		t.Fatalf("seed: %v %+v", err, errs)
	}
	id := strconv.FormatInt(b.ID, 10)

	code, out = f.apiCall(http.MethodGet, "/api/v1/birthdays", raw, "")
	if code != http.StatusOK || out["birthdays"].([]any)[0].(map[string]any)["name"] != "Eben" {
		t.Fatalf("index: %d %+v", code, out)
	}

	code, out = f.apiCall(http.MethodPatch, "/api/v1/birthdays/"+id, raw, `{"birthday":{"year":1991}}`)
	if code != http.StatusOK || out["birthday"].(map[string]any)["year"] != float64(1991) {
		t.Fatalf("patch: %d %+v", code, out)
	}

	code, _ = f.apiCall(http.MethodDelete, "/api/v1/birthdays/"+id, raw, "")
	if code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	_ = u
}

func TestAPICrossUser404(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	e, _, _ := f.store.CreateEvent(u.ID, newEventIn("Secret", "2026-09-25", "2026-09-25"))
	other := f.seedUser("other@example.com", "secret-password")
	_, otherRaw, _ := f.store.CreateToken(other.ID, "other")
	code, _ := f.apiCall(http.MethodGet, "/api/v1/events/"+strconv.FormatInt(e.ID, 10), otherRaw, "")
	if code != http.StatusNotFound {
		t.Fatalf("cross-user: %d", code)
	}
	back, _ := f.store.FindEvent(u.ID, e.ID)
	if back.Title != "Secret" {
		t.Fatal("record touched")
	}
}

func TestAPIRevokedToken(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	tok, raw, _ := f.store.CreateToken(u.ID, "hermes")
	_ = f.store.DeleteToken(u.ID, tok.ID)
	if code, _ := f.apiCall(http.MethodGet, "/api/v1/events", raw, ""); code != http.StatusUnauthorized {
		t.Fatalf("revoked: %d", code)
	}
}

func TestAPIWorksWhileLocked(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	_, raw, _ := f.store.CreateToken(u.ID, "hermes")
	f.login(u.Email, "secret-password")
	u2, _ := url.Parse(f.server.URL)
	f.client.Jar.SetCookies(u2, []*http.Cookie{{Name: AutoLockCookie, Value: "1"}})
	if code, _, _ := f.post("/lock", nil, nil); code != http.StatusSeeOther {
		t.Fatalf("lock: %d", code)
	}
	if code, _, _ := f.get("/", nil); code != http.StatusSeeOther {
		t.Fatalf("web while locked: %d", code)
	}
	if code, _ := f.apiCall(http.MethodGet, "/api/v1/events", raw, ""); code != http.StatusOK {
		t.Fatalf("api while locked: %d", code)
	}
}

func TestAPITouchLastUsed(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	tok, raw, _ := f.store.CreateToken(u.ID, "hermes")
	if tok.LastUsedAt != nil {
		t.Fatal("last used preset")
	}
	if code, _ := f.apiCall(http.MethodGet, "/api/v1/events", raw, ""); code != http.StatusOK {
		t.Fatalf("index: %d", code)
	}
	back, _ := f.store.FindToken(u.ID, tok.ID)
	if back.LastUsedAt == nil {
		t.Fatal("last used not recorded")
	}
}

func TestAPIRateLimit(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	_, raw, _ := f.store.CreateToken(u.ID, "hermes")
	var last int
	for i := 0; i < 61; i++ {
		last, _ = f.apiCall(http.MethodPost, "/api/v1/events", raw, `{"title":"x","starts_on":"2026-09-26"}`)
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("61st write: %d", last)
	}
	if code, _ := f.apiCall(http.MethodGet, "/api/v1/events", raw, ""); code != http.StatusOK {
		t.Fatalf("read after limit: %d", code)
	}
}
