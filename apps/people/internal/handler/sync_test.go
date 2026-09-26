package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aquasp/kurapeople/internal/config"
	"github.com/aquasp/kurapeople/internal/store"
)

func TestPushBirthdayReachesCalendar(t *testing.T) {
	var body, secret string
	cal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body = string(raw)
		secret = r.Header.Get("X-Kura-Sync")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer cal.Close()
	f := newFlow(t, func(c *config.Config) {
		c.CalendarURL = cal.URL
		c.SyncSecret = "sync-secret"
	})
	u := f.seedUser("ada@example.com", "password123")
	person, errs, err := f.store.CreatePerson(u.ID, store.PersonInput{
		Name: "Ada", BirthMonth: "3", BirthDay: "14", BirthYear: "1990",
	})
	if err != nil || len(errs) > 0 {
		t.Fatalf("person: %v %+v", err, errs)
	}
	f.srv.pushBirthday(u, person, false)
	if secret != "sync-secret" {
		t.Fatalf("secret %q", secret)
	}
	for _, want := range []string{`"source_key":"people:`, `"name":"Ada"`, `"month":3`, `"delete":false`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body %s missing %s", body, want)
		}
	}
}
