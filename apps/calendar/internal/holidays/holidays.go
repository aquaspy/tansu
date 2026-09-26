// Package holidays ports app/services/holidays*.rb: the Easter
// computation, weekday helpers, and the BR/US/SI/CZ packs as Go data +
// movable-feast rules.
package holidays

import (
	"sort"
	"time"
)

// Codes mirrors Holidays::CODES.
var Codes = []string{"BR", "US", "SI", "CZ"}

// Holiday is one public holiday: its date, pack key (e.g. "independence"),
// and country code ("BR").
type Holiday struct {
	Date    time.Time
	Key     string
	Country string
}

func item(year, month, day int, key, country string) Holiday {
	return Holiday{Date: time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC), Key: key, Country: country}
}

// Easter computes Western Easter Sunday via the Anonymous Gregorian
// algorithm (Meeus/Jones/Butcher), mirroring Holidays.easter.
func Easter(year int) time.Time {
	a := year % 19
	b, c := year/100, year%100
	d, e := b/4, b%4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month, day := (h+l-7*m+114)/31, (h+l-7*m+114)%31
	return time.Date(year, time.Month(month), day+1, 0, 0, 0, 0, time.UTC)
}

// NthWday is the nth wday (Sunday=0) of the month, mirroring
// Holidays.nth_wday.
func NthWday(year, month int, wday time.Weekday, nth int) time.Time {
	date := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	date = date.AddDate(0, 0, (int(wday)-int(date.Weekday())+7)%7)
	return date.AddDate(0, 0, (nth-1)*7)
}

// LastWday is the last wday (Sunday=0) of the month, mirroring
// Holidays.last_wday.
func LastWday(year, month int, wday time.Weekday) time.Time {
	date := time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC)
	return date.AddDate(0, 0, -(int(date.Weekday())-int(wday)+7)%7)
}

func shift(d time.Time, days int) time.Time { return d.AddDate(0, 0, days) }

// Brazil mirrors Holidays::Brazil.for_year.
func Brazil(year int) []Holiday {
	paschal := Easter(year)
	return []Holiday{
		item(year, 1, 1, "new_year", "BR"),
		{Date: shift(paschal, -47), Key: "carnival", Country: "BR"},
		{Date: shift(paschal, -2), Key: "good_friday", Country: "BR"},
		{Date: paschal, Key: "easter", Country: "BR"},
		item(year, 4, 21, "tiradentes", "BR"),
		item(year, 5, 1, "labor", "BR"),
		{Date: shift(paschal, 60), Key: "corpus_christi", Country: "BR"},
		item(year, 9, 7, "independence", "BR"),
		item(year, 10, 12, "aparecida", "BR"),
		item(year, 11, 2, "finados", "BR"),
		item(year, 11, 15, "republic", "BR"),
		item(year, 11, 20, "black_awareness", "BR"),
		item(year, 12, 25, "christmas", "BR"),
	}
}

// UnitedStates mirrors Holidays::UnitedStates.for_year.
func UnitedStates(year int) []Holiday {
	return []Holiday{
		item(year, 1, 1, "new_year", "US"),
		{Date: NthWday(year, 1, time.Monday, 3), Key: "mlk", Country: "US"},
		{Date: NthWday(year, 2, time.Monday, 3), Key: "presidents", Country: "US"},
		{Date: LastWday(year, 5, time.Monday), Key: "memorial", Country: "US"},
		item(year, 6, 19, "juneteenth", "US"),
		item(year, 7, 4, "independence", "US"),
		{Date: NthWday(year, 9, time.Monday, 1), Key: "labor", Country: "US"},
		{Date: NthWday(year, 10, time.Monday, 2), Key: "columbus", Country: "US"},
		item(year, 11, 11, "veterans", "US"),
		{Date: NthWday(year, 11, time.Thursday, 4), Key: "thanksgiving", Country: "US"},
		item(year, 12, 25, "christmas", "US"),
	}
}

// Slovenia mirrors Holidays::Slovenia.for_year.
func Slovenia(year int) []Holiday {
	paschal := Easter(year)
	return []Holiday{
		item(year, 1, 1, "new_year", "SI"),
		item(year, 1, 2, "new_year_2", "SI"),
		item(year, 2, 8, "preseren", "SI"),
		{Date: paschal, Key: "easter", Country: "SI"},
		{Date: shift(paschal, 1), Key: "easter_monday", Country: "SI"},
		item(year, 4, 27, "uprising", "SI"),
		item(year, 5, 1, "labor", "SI"),
		item(year, 5, 2, "labor_2", "SI"),
		{Date: shift(paschal, 49), Key: "pentecost", Country: "SI"},
		item(year, 6, 25, "statehood", "SI"),
		item(year, 8, 15, "assumption", "SI"),
		item(year, 10, 31, "reformation", "SI"),
		item(year, 11, 1, "all_saints", "SI"),
		item(year, 12, 25, "christmas", "SI"),
		item(year, 12, 26, "independence", "SI"),
	}
}

// Czechia mirrors Holidays::Czechia.for_year.
func Czechia(year int) []Holiday {
	paschal := Easter(year)
	return []Holiday{
		item(year, 1, 1, "new_year", "CZ"),
		{Date: shift(paschal, -2), Key: "good_friday", Country: "CZ"},
		{Date: shift(paschal, 1), Key: "easter_monday", Country: "CZ"},
		item(year, 5, 1, "labor", "CZ"),
		item(year, 5, 8, "liberation", "CZ"),
		item(year, 7, 5, "cyril_methodius", "CZ"),
		item(year, 7, 6, "jan_hus", "CZ"),
		item(year, 9, 28, "statehood", "CZ"),
		item(year, 10, 28, "czechoslovak", "CZ"),
		item(year, 11, 17, "freedom", "CZ"),
		item(year, 12, 24, "christmas_eve", "CZ"),
		item(year, 12, 25, "christmas", "CZ"),
		item(year, 12, 26, "st_stephen", "CZ"),
	}
}

var packs = map[string]func(int) []Holiday{
	"BR": Brazil,
	"US": UnitedStates,
	"SI": Slovenia,
	"CZ": Czechia,
}

// ForYear returns every holiday of one pack in a year.
func ForYear(country string, year int) []Holiday {
	if pack, ok := packs[country]; ok {
		return pack(year)
	}
	return nil
}

func between(d, from, to time.Time) bool {
	return !d.Before(from) && !d.After(to)
}

// InRange mirrors Holidays.in_range: the selected packs' holidays within
// [from, to], sorted by date, country, key.
func InRange(countries []string, from, to time.Time) []Holiday {
	seen := map[string]bool{}
	var out []Holiday
	for _, code := range countries {
		if seen[code] {
			continue
		}
		seen[code] = true
		pack, ok := packs[code]
		if !ok {
			continue
		}
		for year := from.Year(); year <= to.Year(); year++ {
			for _, h := range pack(year) {
				if between(h.Date, from, to) {
					out = append(out, h)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Date.Equal(out[j].Date) {
			return out[i].Date.Before(out[j].Date)
		}
		if out[i].Country != out[j].Country {
			return out[i].Country < out[j].Country
		}
		return out[i].Key < out[j].Key
	})
	return out
}
