package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/aquasp/kuracalendar/internal/config"
)

func TestBirthdaySyncUpsertsAndDeletes(t *testing.T) {
	f := newFlow(t, func(c *config.Config) { c.SyncSecret = "sync-secret" })
	f.seedUser("ada@example.com", "secret-password")
	post := func(body, secret string) int {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, f.server.URL+"/sync/birthdays", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Kura-Sync", secret)
		resp, err := f.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	body := `{"email":"ada@example.com","source_key":"people:7","name":"Ada","emoji":"🎂","month":3,"day":14,"year":1990}`
	if code := post(body, "nope"); code != http.StatusUnauthorized {
		t.Fatalf("bad secret: %d", code)
	}
	if code := post(body, "sync-secret"); code != http.StatusNoContent {
		t.Fatalf("create: %d", code)
	}
	if code := post(body, "sync-secret"); code != http.StatusNoContent {
		t.Fatalf("update: %d", code)
	}
	u, err := f.store.FindUserByEmail("ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := f.store.ListBirthdays(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "Ada" || rows[0].Month != 3 || rows[0].Day != 14 {
		t.Fatalf("birthdays: %+v", rows)
	}
	if code := post(`{"email":"ada@example.com","source_key":"people:7","delete":true}`, "sync-secret"); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	rows, _ = f.store.ListBirthdays(u.ID)
	if len(rows) != 0 {
		t.Fatalf("after delete: %+v", rows)
	}
}
