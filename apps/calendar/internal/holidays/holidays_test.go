package holidays

import (
	"testing"
	"time"
)

func date(y, m, d int) time.Time {
	return time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
}

func byKey(t *testing.T, list []Holiday) map[string]Holiday {
	t.Helper()
	out := map[string]Holiday{}
	for _, h := range list {
		out[h.Key] = h
	}
	return out
}

func TestEasterKnownYears(t *testing.T) {
	cases := map[int]time.Time{
		2026: date(2026, 4, 5),
		2025: date(2025, 4, 20),
		2024: date(2024, 3, 31),
		2000: date(2000, 4, 23),
	}
	for year, want := range cases {
		if got := Easter(year); !got.Equal(want) {
			t.Errorf("Easter(%d) = %s, want %s", year, got.Format("2006-01-02"), want.Format("2006-01-02"))
		}
	}
}

func TestBrazilCarnival2026(t *testing.T) {
	got := byKey(t, Brazil(2026))["carnival"].Date
	if !got.Equal(date(2026, 2, 17)) {
		t.Fatalf("carnival = %s", got.Format("2006-01-02"))
	}
}

func TestBrazilGoodFridayAndCorpusChristi(t *testing.T) {
	by := byKey(t, Brazil(2026))
	if got := by["good_friday"].Date; !got.Equal(date(2026, 4, 3)) {
		t.Errorf("good friday = %s", got.Format("2006-01-02"))
	}
	if got := by["corpus_christi"].Date; !got.Equal(date(2026, 6, 4)) {
		t.Errorf("corpus christi = %s", got.Format("2006-01-02"))
	}
}

func TestBrazilIndependenceInRange(t *testing.T) {
	found := InRange([]string{"BR"}, date(2026, 9, 1), date(2026, 9, 30))
	ok := false
	for _, h := range found {
		if h.Key == "independence" && h.Date.Equal(date(2026, 9, 7)) {
			ok = true
		}
	}
	if !ok {
		t.Fatal("independence day missing from september 2026")
	}
}

func TestUnitedStatesMovable2026(t *testing.T) {
	by := byKey(t, UnitedStates(2026))
	want := map[string]time.Time{
		"mlk":          date(2026, 1, 19),
		"presidents":   date(2026, 2, 16),
		"memorial":     date(2026, 5, 25),
		"independence": date(2026, 7, 4),
		"labor":        date(2026, 9, 7),
		"thanksgiving": date(2026, 11, 26),
	}
	for key, day := range want {
		if got := by[key].Date; !got.Equal(day) {
			t.Errorf("%s = %s, want %s", key, got.Format("2006-01-02"), day.Format("2006-01-02"))
		}
	}
}

func TestSlovenia2026(t *testing.T) {
	by := byKey(t, Slovenia(2026))
	want := map[string]time.Time{
		"new_year_2":    date(2026, 1, 2),
		"easter_monday": date(2026, 4, 6),
		"pentecost":     date(2026, 5, 24),
		"statehood":     date(2026, 6, 25),
	}
	for key, day := range want {
		if got := by[key].Date; !got.Equal(day) {
			t.Errorf("%s = %s, want %s", key, got.Format("2006-01-02"), day.Format("2006-01-02"))
		}
	}
}

func TestCzechia2026(t *testing.T) {
	by := byKey(t, Czechia(2026))
	want := map[string]time.Time{
		"good_friday":   date(2026, 4, 3),
		"easter_monday": date(2026, 4, 6),
		"jan_hus":       date(2026, 7, 6),
		"christmas_eve": date(2026, 12, 24),
	}
	for key, day := range want {
		if got := by[key].Date; !got.Equal(day) {
			t.Errorf("%s = %s, want %s", key, got.Format("2006-01-02"), day.Format("2006-01-02"))
		}
	}
}

func TestInRangeMergesCountries(t *testing.T) {
	day := date(2026, 9, 7)
	found := InRange([]string{"BR", "US"}, day, day)
	keys := map[[2]string]bool{}
	for _, h := range found {
		keys[[2]string{h.Country, h.Key}] = true
	}
	if !keys[[2]string{"BR", "independence"}] {
		t.Error("missing BR independence")
	}
	if !keys[[2]string{"US", "labor"}] {
		t.Error("missing US labor")
	}
}

func TestInRangeSkipsUnknownPacks(t *testing.T) {
	if got := InRange([]string{"XX"}, date(2026, 1, 1), date(2026, 12, 31)); len(got) != 0 {
		t.Fatalf("unknown pack returned %d holidays", len(got))
	}
}
