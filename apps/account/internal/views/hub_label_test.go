package views

import (
	"testing"

	"github.com/aquasp/kuraaccount/internal/i18n"
	"github.com/aquasp/kuraaccount/internal/store"
)

func TestAppLabelFirstPartyAndOther(t *testing.T) {
	en := Page{L: i18n.EN}
	pt := Page{L: i18n.PT}
	if got := AppLabel(en, &store.Client{ID: "kurapeople", Name: "TansuPeople", Icon: "🧑"}); got != "People" {
		t.Fatalf("people: %q", got)
	}
	if got := AppLabel(pt, &store.Client{ID: "kuracalendar", Name: "Tansu Calendar"}); got != "Agenda" {
		t.Fatalf("calendar: %q", got)
	}
	if got := AppLabel(en, &store.Client{ID: "kuraemail", Name: "Tansu Email"}); got != "Email" {
		t.Fatalf("email: %q", got)
	}
	if got := AppLabel(pt, &store.Client{ID: "kuraspend", Name: "Tansu Spend"}); got != "Gastos" {
		t.Fatalf("spend: %q", got)
	}
	if got := AppLabel(en, &store.Client{ID: "kuranotes", Name: "Tansu Notes"}); got != "Notes" {
		t.Fatalf("notes: %q", got)
	}
	if got := AppLabel(pt, &store.Client{ID: "kurachat", Name: "Tansu Assistant"}); got != "Assistente" {
		t.Fatalf("assistant: %q", got)
	}
	other := &store.Client{ID: "demo", Name: "Lab", Icon: "🧪"}
	if FirstParty(other) {
		t.Fatal("demo is not first-party")
	}
	if got := AppLabel(en, other); got != "Lab" {
		t.Fatalf("other: %q", got)
	}
	if !FirstParty(&store.Client{ID: "kuraemail"}) {
		t.Fatal("email should be first-party")
	}
}

func TestTimezoneReady(t *testing.T) {
	if !TimezoneReady(HubData{Timezone: "UTC"}) {
		t.Fatal("UTC is a saved zone")
	}
	if !TimezoneReady(HubData{Timezone: "America/Sao_Paulo"}) {
		t.Fatal("sao paulo")
	}
	if TimezoneReady(HubData{}) {
		t.Fatal("empty should lead")
	}
	if TimezoneReady(HubData{Timezone: "   "}) {
		t.Fatal("blank should lead")
	}
	if TimezoneReady(HubData{Timezone: "BRT", TimezoneError: "hub.timezone_invalid"}) {
		t.Fatal("invalid should lead")
	}
}
