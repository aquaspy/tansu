package i18n

import (
	"testing"
	"time"

	_ "time/tzdata"
)

func TestTimeShortUsesLocation(t *testing.T) {
	prev := time.Local
	fixed := time.FixedZone("UTC+9", 9*3600)
	time.Local = fixed
	t.Cleanup(func() { time.Local = prev })

	at := time.Date(2026, 9, 28, 15, 0, 0, 0, time.UTC)
	if got := TimeShort(EN, at, nil); got != "Sep 28, 15:00" {
		t.Fatalf("nil loc = %q", got)
	}
	sp, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	if got := TimeShort(EN, at, sp); got != "Sep 28, 12:00" {
		t.Fatalf("sao paulo = %q", got)
	}
	if got := TimeShort(PT, at, sp); got != "28/09, 12:00" {
		t.Fatalf("pt = %q", got)
	}
}
