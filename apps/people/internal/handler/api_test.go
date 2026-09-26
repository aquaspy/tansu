package handler

import (
	"net/http"
	"strconv"
	"testing"
)

func TestAPIUnauthorized(t *testing.T) {
	f := newFlow(t, nil)
	if code, out := f.apiCall(http.MethodGet, "/api/v1/people", "", ""); code != http.StatusUnauthorized || out["error"] != "unauthorized" {
		t.Fatalf("no token: %d %+v", code, out)
	}
	if code, _ := f.apiCall(http.MethodGet, "/api/v1/people", "kura_nope", ""); code != http.StatusUnauthorized {
		t.Fatalf("bad token: %d", code)
	}
}

func TestAPIPeopleCRUD(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	_, raw, _ := f.store.CreateToken(u.ID, "hermes")

	code, out := f.apiCall(http.MethodPost, "/api/v1/people", raw, `{"person":{
		"name": "Ada Lovelace", "nickname": "Ada", "relationship": "friend",
		"birthday": {"month": 8, "day": 11, "year": 2000},
		"sizes": {"ring": "16", "shoe": "37"},
		"height": "1.65m", "favorites": "tea",
		"attrs": [{"label": "Coffee", "value": "flat white"}]
	}}`)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %+v", code, out)
	}
	created := out["person"].(map[string]any)
	if created["name"] != "Ada Lovelace" || created["height"] != "1.65m" {
		t.Fatalf("shape: %+v", created)
	}
	bday := created["birthday"].(map[string]any)
	if bday["month"] != 8.0 || bday["day"] != 11.0 || bday["year"] != 2000.0 {
		t.Fatalf("birthday: %+v", bday)
	}
	sizes := created["sizes"].(map[string]any)
	if sizes["ring"] != "16" {
		t.Fatalf("sizes: %+v", sizes)
	}
	attrs := created["attrs"].([]any)
	if len(attrs) != 1 || attrs[0].(map[string]any)["value"] != "flat white" {
		t.Fatalf("attrs: %+v", attrs)
	}
	if created["countdown"] == nil {
		t.Fatalf("countdown missing: %+v", created)
	}
	id := strconv.FormatInt(int64(created["id"].(float64)), 10)

	// Flat params work too, and PATCH merges onto the record.
	code, out = f.apiCall(http.MethodPatch, "/api/v1/people/"+id, raw,
		`{"nickname": "A.", "shoe_size": "38"}`)
	if code != http.StatusOK {
		t.Fatalf("update: %d %+v", code, out)
	}
	updated := out["person"].(map[string]any)
	if updated["nickname"] != "A." || updated["name"] != "Ada Lovelace" {
		t.Fatalf("merge: %+v", updated)
	}
	if updated["sizes"].(map[string]any)["shoe"] != "38" {
		t.Fatalf("sizes merge: %+v", updated)
	}
	// Attrs untouched when the key is absent.
	if len(updated["attrs"].([]any)) != 1 {
		t.Fatalf("attrs dropped: %+v", updated)
	}

	code, out = f.apiCall(http.MethodGet, "/api/v1/people/"+id, raw, "")
	if code != http.StatusOK {
		t.Fatalf("show: %d %+v", code, out)
	}

	code, _ = f.apiCall(http.MethodDelete, "/api/v1/people/"+id, raw, "")
	if code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if code, _ := f.apiCall(http.MethodGet, "/api/v1/people/"+id, raw, ""); code != http.StatusNotFound {
		t.Fatalf("show after delete: %d", code)
	}
}

func TestAPIPeopleFilters(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	_, raw, _ := f.store.CreateToken(u.ID, "hermes")
	f.apiCall(http.MethodPost, "/api/v1/people", raw,
		`{"name": "Ada Lovelace", "relationship": "friend", "birthday_month": 8, "birthday_day": 11}`)
	f.apiCall(http.MethodPost, "/api/v1/people", raw,
		`{"name": "Lin", "relationship": "family", "birthday_month": 12, "birthday_day": 1}`)

	code, out := f.apiCall(http.MethodGet, "/api/v1/people?q=lovelace", raw, "")
	if code != http.StatusOK || len(out["people"].([]any)) != 1 {
		t.Fatalf("q: %d %+v", code, out)
	}
	code, out = f.apiCall(http.MethodGet, "/api/v1/people?relationship=family", raw, "")
	if code != http.StatusOK || len(out["people"].([]any)) != 1 {
		t.Fatalf("relationship: %d %+v", code, out)
	}
	code, out = f.apiCall(http.MethodGet, "/api/v1/people?birthday_month=12", raw, "")
	if code != http.StatusOK || len(out["people"].([]any)) != 1 {
		t.Fatalf("month: %d %+v", code, out)
	}
	code, out = f.apiCall(http.MethodGet, "/api/v1/people?limit=1", raw, "")
	if code != http.StatusOK || len(out["people"].([]any)) != 1 {
		t.Fatalf("limit: %d %+v", code, out)
	}
}

func TestAPIPeopleScopedAndValidated(t *testing.T) {
	f := newFlow(t, nil)
	a := f.seedUser("a@example.com", "secret-password")
	b := f.seedUser("b@example.com", "secret-password")
	_, rawA, _ := f.store.CreateToken(a.ID, "hermes")
	_, rawB, _ := f.store.CreateToken(b.ID, "hermes")
	_, out := f.apiCall(http.MethodPost, "/api/v1/people", rawA, `{"name": "Secret"}`)
	id := strconv.FormatInt(int64(out["person"].(map[string]any)["id"].(float64)), 10)
	if code, _ := f.apiCall(http.MethodGet, "/api/v1/people/"+id, rawB, ""); code != http.StatusNotFound {
		t.Fatalf("stranger show: %d", code)
	}
	if code, _ := f.apiCall(http.MethodGet, "/api/v1/people", rawB, ""); code != http.StatusOK {
		t.Fatalf("stranger index: %d", code)
	}

	code, out := f.apiCall(http.MethodPost, "/api/v1/people", rawA, `{"name": "  "}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("blank name: %d %+v", code, out)
	}
	if errs, ok := out["errors"].([]any); !ok || len(errs) != 1 || errs[0] != "Name can't be blank" {
		t.Fatalf("errors: %+v", out)
	}

	code, out = f.apiCall(http.MethodPost, "/api/v1/people", rawA,
		`{"name": "Bad", "birthday": {"month": 4, "day": 31}}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("bad birthday: %d %+v", code, out)
	}
}
