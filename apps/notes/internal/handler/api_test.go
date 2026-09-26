package handler

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"
)

func TestAPINotes(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("you@example.com", "password1")
	_, raw, err := f.store.CreateToken(u.ID, "agent")
	if err != nil {
		t.Fatal(err)
	}

	// Unauthorized shapes.
	if code, out := f.apiCall(http.MethodGet, "/api/v1/notes", "", ""); code != http.StatusUnauthorized || out["error"] != "unauthorized" {
		t.Fatalf("no token: %d %+v", code, out)
	}
	if code, _ := f.apiCall(http.MethodGet, "/api/v1/notes", "kura_nope", ""); code != http.StatusUnauthorized {
		t.Fatalf("bad token: %d", code)
	}

	// Create nested + flat.
	code, out := f.apiCall(http.MethodPost, "/api/v1/notes", raw, `{"note":{"body":"Trip\nKyoto","folder":"travel"}}`)
	if code != http.StatusCreated {
		t.Fatalf("create nested: %d %+v", code, out)
	}
	note := out["note"].(map[string]any)
	if note["title"] != "Trip" || note["preview"] != "Kyoto" || note["folder"] != "travel" || note["shared"] != false {
		t.Fatalf("shape: %+v", note)
	}
	id := strconv.FormatFloat(note["id"].(float64), 'f', 0, 64)

	code, out = f.apiCall(http.MethodPost, "/api/v1/notes", raw, `{"body":"Flat note"}`)
	if code != http.StatusCreated {
		t.Fatalf("create flat: %d %+v", code, out)
	}

	// Index with filters.
	_, out = f.apiCall(http.MethodGet, "/api/v1/notes?limit=200", raw, "")
	if len(out["notes"].([]any)) != 2 {
		t.Fatalf("index: %+v", out)
	}
	_, out = f.apiCall(http.MethodGet, "/api/v1/notes?folder=travel&q=kyoto", raw, "")
	if len(out["notes"].([]any)) != 1 {
		t.Fatalf("filtered: %+v", out)
	}

	// Show / update / delete.
	code, out = f.apiCall(http.MethodGet, "/api/v1/notes/"+id, raw, "")
	if code != http.StatusOK {
		t.Fatalf("show: %d", code)
	}
	code, out = f.apiCall(http.MethodPatch, "/api/v1/notes/"+id, raw, `{"folder":"done"}`)
	if code != http.StatusOK || out["note"].(map[string]any)["folder"] != "done" {
		t.Fatalf("move: %d %+v", code, out)
	}
	code, _ = f.apiCall(http.MethodDelete, "/api/v1/notes/"+id, raw, "")
	if code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	code, out = f.apiCall(http.MethodGet, "/api/v1/notes/"+id, raw, "")
	if code != http.StatusNotFound || out["error"] != "not_found" {
		t.Fatalf("deleted show: %d %+v", code, out)
	}
	if code, _ := f.apiCall(http.MethodGet, "/api/v1/notes/abc", raw, ""); code != http.StatusNotFound {
		t.Fatalf("bad id: %d", code)
	}
}

func TestAPIFolders(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("you@example.com", "password1")
	_, raw, _ := f.store.CreateToken(u.ID, "agent")
	if _, err := f.store.CreateNote(u.ID, "a", "work"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.CreateNote(u.ID, "b", ""); err != nil {
		t.Fatal(err)
	}

	_, out := f.apiCall(http.MethodGet, "/api/v1/folders", raw, "")
	folders := out["folders"].([]any)
	if len(folders) != 2 {
		t.Fatalf("index: %+v", out)
	}

	code, out := f.apiCall(http.MethodPatch, "/api/v1/folders", raw, `{"from":"work","to":"job"}`)
	if code != http.StatusOK || out["folder"] != "job" {
		t.Fatalf("rename: %d %+v", code, out)
	}
	code, out = f.apiCall(http.MethodPatch, "/api/v1/folders", raw, `{"from":"job","to":"inbox"}`)
	if code != http.StatusUnprocessableEntity || len(out["errors"].([]any)) != 1 {
		t.Fatalf("reserved rename: %d %+v", code, out)
	}

	code, out = f.apiCall(http.MethodDelete, "/api/v1/folders?folder=job", raw, "")
	if code != http.StatusOK || out["deleted"] != float64(1) {
		t.Fatalf("clear: %d %+v", code, out)
	}
	code, out = f.apiCall(http.MethodDelete, "/api/v1/folders?folder=all", raw, "")
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("clear all: %d %+v", code, out)
	}
}

func TestAPIRateLimit(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("you@example.com", "password1")
	_, raw, _ := f.store.CreateToken(u.ID, "agent")
	var last int
	for i := 0; i < 61; i++ {
		last, _ = f.apiCall(http.MethodPost, "/api/v1/notes", raw, `{"body":"x"}`)
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("61st write: %d", last)
	}
	// Reads are not limited.
	if code, _ := f.apiCall(http.MethodGet, "/api/v1/notes", raw, ""); code != http.StatusOK {
		t.Fatalf("read after limit: %d", code)
	}
}

func TestAPIWorksWhileLocked(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("you@example.com", "password1")
	_, raw, _ := f.store.CreateToken(u.ID, "agent")
	f.login(u.Email, "password1")
	u2, _ := url.Parse(f.server.URL)
	f.client.Jar.SetCookies(u2, []*http.Cookie{{Name: AutoLockCookie, Value: "1"}})
	if code, _, _ := f.post("/lock", nil, nil); code != http.StatusSeeOther {
		t.Fatalf("lock: %d", code)
	}
	if code, _, _ := f.get("/", nil); code != http.StatusSeeOther {
		t.Fatalf("web while locked: %d", code)
	}
	if code, _ := f.apiCall(http.MethodGet, "/api/v1/notes", raw, ""); code != http.StatusOK {
		t.Fatalf("api while locked: %d", code)
	}
}
