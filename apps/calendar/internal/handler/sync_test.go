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

func TestPaymentDaySyncUpsertsMatchesAndDeletes(t *testing.T) {
	f := newFlow(t, func(c *config.Config) { c.SyncSecret = "sync-secret" })
	u := f.seedUser("ada@example.com", "secret-password")
	if err := f.store.SetUserSub(u.ID, "acct-ada"); err != nil {
		t.Fatal(err)
	}
	post := func(body, secret string) int {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, f.server.URL+"/sync/payment_days", strings.NewReader(body))
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
	body := `{"account_sub":"acct-ada","email":"other@example.com","source_key":"spend:4","title":"Water","notes":"bill","due_day":10}`
	if code := post(body, "nope"); code != http.StatusUnauthorized {
		t.Fatalf("bad secret: %d", code)
	}
	if code := post(body, ""); code != http.StatusUnauthorized {
		t.Fatalf("empty secret: %d", code)
	}
	off := newFlow(t, nil)
	req, _ := http.NewRequest(http.MethodPost, off.server.URL+"/sync/payment_days", strings.NewReader(body))
	req.Header.Set("X-Kura-Sync", "sync-secret")
	resp, err := off.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unset secret: %d", resp.StatusCode)
	}
	if code := post(body, "sync-secret"); code != http.StatusNoContent {
		t.Fatalf("create: %d", code)
	}
	if code := post(`{"email":"ada@example.com","source_key":"spend:4","title":"Water","due_day":12,"notes":"updated"}`, "sync-secret"); code != http.StatusNoContent {
		t.Fatalf("email fallback update: %d", code)
	}
	rows, err := f.store.ListPaymentDays(u.ID)
	if err != nil || len(rows) != 1 || rows[0].DueDay != 12 || rows[0].Notes != "updated" || rows[0].Title != "Water" {
		t.Fatalf("rows: %+v %v", rows, err)
	}
	if code := post(`{"email":"missing@example.com","source_key":"spend:4","title":"Water","due_day":1}`, "sync-secret"); code != http.StatusNotFound {
		t.Fatalf("unknown user: %d", code)
	}
	if code := post(`{"email":"ada@example.com","source_key":"bad key","title":"Water","due_day":1}`, "sync-secret"); code != http.StatusBadRequest {
		t.Fatalf("bad source: %d", code)
	}
	if code := post(`{"email":"ada@example.com","source_key":"spend:4","title":"","due_day":1}`, "sync-secret"); code != http.StatusUnprocessableEntity {
		t.Fatalf("blank title: %d", code)
	}
	if code := post(`{"email":"ada@example.com","source_key":"spend:4","delete":true}`, "sync-secret"); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	rows, _ = f.store.ListPaymentDays(u.ID)
	if len(rows) != 0 {
		t.Fatalf("after delete: %+v", rows)
	}
	if code := post(`{"email":"ada@example.com","source_key":"spend:9","title":"Rent","due_day":0}`, "sync-secret"); code != http.StatusNoContent {
		t.Fatalf("due day 0: %d", code)
	}
	rows, _ = f.store.ListPaymentDays(u.ID)
	if len(rows) != 0 {
		t.Fatalf("zero day stored: %+v", rows)
	}
}
