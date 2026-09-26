package money

import (
	"testing"

	"github.com/aquasp/kuraspend/internal/i18n"
)

func TestToHomeCentsIdentity(t *testing.T) {
	got, err := ToHomeCents(1234, "BRL", "BRL", map[string]string{})
	if err != nil || got != 1234 {
		t.Fatalf("identity: %d %v", got, err)
	}
}

func TestToHomeCentsMultiplies(t *testing.T) {
	got, err := ToHomeCents(1000, "USD", "BRL", map[string]string{"USD": "5.45"})
	if err != nil || got != 5450 {
		t.Fatalf("multiply: %d %v", got, err)
	}
}

func TestToHomeCentsMissingRate(t *testing.T) {
	_, err := ToHomeCents(100, "EUR", "BRL", map[string]string{})
	mr, ok := err.(*MissingRate)
	if !ok || mr.Currency != "EUR" {
		t.Fatalf("missing rate: %v", err)
	}
}

func TestParseCentsLocales(t *testing.T) {
	cases := map[string]int64{
		"12,50":     1250,
		"12.50":     1250,
		"1.234,56":  123456,
		"1,234.56":  123456,
		"12":        1200,
		"-5,00":     -500,
		"10000":     1000000,
		"10.000,00": 1000000,
		"R$ 25.50":  2550,
		"12.5":      1250,
		"1.234":     123400,
	}
	for in, want := range cases {
		got, ok := ParseCents(in)
		if !ok || got != want {
			t.Errorf("ParseCents(%q) = %d, %v; want %d", in, got, ok, want)
		}
	}
	if _, ok := ParseCents(""); ok {
		t.Error("ParseCents(\"\") should fail")
	}
	if _, ok := ParseCents("abc"); ok {
		t.Error("ParseCents(\"abc\") should fail")
	}
}

func TestFormatLocales(t *testing.T) {
	if got := Format(123456, "BRL", i18n.PT); got != "R$ 1.234,56" {
		t.Errorf("pt format: %q", got)
	}
	if got := Format(123456, "USD", i18n.EN); got != "$1,234.56" {
		t.Errorf("en format: %q", got)
	}
	if got := Format(123456, "EUR", i18n.EN); got != "€1,234.56" {
		t.Errorf("en eur: %q", got)
	}
	if got := Format(-500, "BRL", i18n.PT); got != "R$ -5,00" {
		t.Errorf("pt negative: %q", got)
	}
	if got := Format(100, "XXX", i18n.EN); got != "1.00 XXX" {
		t.Errorf("unknown currency: %q", got)
	}
}

func TestInputAmount(t *testing.T) {
	if got := InputAmount(1250, i18n.PT); got != "12,50" {
		t.Errorf("pt input: %q", got)
	}
	if got := InputAmount(1250, i18n.EN); got != "12.50" {
		t.Errorf("en input: %q", got)
	}
	if got := InputAmount(-500, i18n.EN); got != "-5.00" {
		t.Errorf("negative input: %q", got)
	}
}

func TestRoundingHalfUp(t *testing.T) {
	// 1 cent * 0.5 = 0.5 -> ties round up to 1.
	got, err := ToHomeCents(1, "USD", "BRL", map[string]string{"USD": "0.5"})
	if err != nil || got != 1 {
		t.Fatalf("half up: %d %v", got, err)
	}
	// 1 cent * 0.49 -> 0.
	got, err = ToHomeCents(1, "USD", "BRL", map[string]string{"USD": "0.49"})
	if err != nil || got != 0 {
		t.Fatalf("round down: %d %v", got, err)
	}
	// 100 cents * 5.455 = 545.5 -> 546.
	got, err = ToHomeCents(100, "USD", "BRL", map[string]string{"USD": "5.455"})
	if err != nil || got != 546 {
		t.Fatalf("fraction: %d %v", got, err)
	}
}

func TestValidRate(t *testing.T) {
	for _, s := range []string{"5.45", "1", "0.5", "1e2"} {
		if !ValidRate(s) {
			t.Errorf("ValidRate(%q) = false", s)
		}
	}
	for _, s := range []string{"", " ", "0", "-3", "abc", "5,45", "1/2"} {
		if ValidRate(s) {
			t.Errorf("ValidRate(%q) = true", s)
		}
	}
}

func TestValidCurrency(t *testing.T) {
	for _, c := range []string{"BRL", "usd", " EUR "} {
		if !ValidCurrency(c) {
			t.Errorf("ValidCurrency(%q) = false", c)
		}
	}
	if ValidCurrency("GBP") || ValidCurrency("") {
		t.Error("ValidCurrency accepted junk")
	}
}
