package i18n

import (
	"testing"
	"time"
)

func TestTimeShortNilIsUTC(t *testing.T) {
	prev := time.Local
	time.Local = time.FixedZone("UTC+9", 9*3600)
	t.Cleanup(func() { time.Local = prev })
	at := time.Date(2026, 9, 28, 15, 0, 0, 0, time.UTC)
	if got := TimeShort(EN, at, nil); got != "Sep 28, 15:00" {
		t.Fatalf("nil loc = %q", got)
	}
}
