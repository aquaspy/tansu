// Package money ports app/services/money.rb: parsing, formatting, and
// home-currency conversion. All arithmetic is exact (big.Rat) with
// Ruby BigDecimal#round semantics (half up, toward +∞ on ties).
package money

import (
	"math/big"
	"regexp"
	"strings"

	"github.com/aquasp/kuraspend/internal/i18n"
)

// Currencies mirrors Money::CURRENCIES.
var Currencies = []string{"BRL", "USD", "EUR"}

var symbols = map[string]string{"BRL": "R$", "USD": "$", "EUR": "€"}

// ValidCurrency reports whether code is a supported currency.
func ValidCurrency(code string) bool {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "BRL", "USD", "EUR":
		return true
	}
	return false
}

// MissingRate mirrors Money::MissingRate.
type MissingRate struct {
	Currency string
}

func (e *MissingRate) Error() string { return "missing rate for " + e.Currency }

// ToHomeCents mirrors Money.to_home_cents: identity when from == home,
// otherwise cents * rate rounded to the nearest cent.
func ToHomeCents(amountCents int64, from, home string, rates map[string]string) (int64, error) {
	from = strings.ToUpper(from)
	home = strings.ToUpper(home)
	if from == home {
		return amountCents, nil
	}
	raw, ok := rates[from]
	if !ok || strings.TrimSpace(raw) == "" {
		return 0, &MissingRate{Currency: from}
	}
	rate, ok := parseDecimal(strings.TrimSpace(raw))
	if !ok {
		return 0, &MissingRate{Currency: from}
	}
	r := new(big.Rat).Mul(big.NewRat(amountCents, 1), rate)
	return roundHalfUp(r)
}

// ValidRate reports whether s parses as a positive decimal, mirroring the
// BigDecimal check in User#fx_rates_make_sense.
func ValidRate(s string) bool {
	r, ok := parseDecimal(strings.TrimSpace(s))
	return ok && r.Sign() > 0
}

// ParseCents mirrors Money.parse_cents: locale-agnostic decimal input to
// integer cents. ok == false means nil (blank or unparseable).
func ParseCents(value string) (cents int64, ok bool) {
	str := strings.TrimSpace(value)
	if str == "" {
		return 0, false
	}
	negative := strings.HasPrefix(str, "-")
	var cleaned strings.Builder
	for _, r := range str {
		if r >= '0' && r <= '9' || r == ',' || r == '.' {
			cleaned.WriteRune(r)
		}
	}
	if cleaned.Len() == 0 {
		return 0, false
	}
	normalized := normalizeDecimal(cleaned.String())
	r, valid := parseDecimal(normalized)
	if !valid {
		return 0, false
	}
	r.Mul(r, big.NewRat(100, 1))
	cents, err := roundHalfUp(r)
	if err != nil {
		return 0, false
	}
	if negative {
		cents = -cents
	}
	return cents, true
}

// Format mirrors Money.format.
func Format(cents int64, currency string, l i18n.Locale) string {
	currency = strings.ToUpper(currency)
	number := FormatNumber(cents, l)
	symbol, known := symbols[currency]
	if !known {
		return number + " " + currency
	}
	if l == i18n.PT {
		return symbol + " " + number
	}
	return symbol + number
}

// InputAmount mirrors Money.input_amount: plain "12.50" / "12,50" for forms.
func InputAmount(cents int64, l i18n.Locale) string {
	sign := ""
	abs := cents
	if cents < 0 {
		sign = "-"
		abs = -cents
	}
	sep := "."
	if l == i18n.PT {
		sep = ","
	}
	return sign + itoa(abs/100) + sep + pad2(abs%100)
}

// FormatNumber mirrors Money.format_number: grouped whole part + 2 decimals.
func FormatNumber(cents int64, l i18n.Locale) string {
	sign := ""
	abs := cents
	if cents < 0 {
		sign = "-"
		abs = -cents
	}
	if l == i18n.PT {
		return sign + delimit(abs/100, ".") + "," + pad2(abs%100)
	}
	return sign + delimit(abs/100, ",") + "." + pad2(abs%100)
}

// delimit mirrors Money.delimit: thousands grouping.
func delimit(whole int64, separator string) string {
	digits := itoa(whole)
	n := len(digits)
	if n <= 3 {
		return digits
	}
	// Group from the right, like the reversed-gsub in Ruby.
	var b strings.Builder
	lead := n % 3
	if lead == 0 {
		lead = 3
	}
	b.WriteString(digits[:lead])
	for i := lead; i < n; i += 3 {
		b.WriteString(separator)
		b.WriteString(digits[i : i+3])
	}
	return b.String()
}

// normalizeDecimal mirrors Money.normalize_decimal.
func normalizeDecimal(cleaned string) string {
	comma := strings.LastIndex(cleaned, ",")
	dot := strings.LastIndex(cleaned, ".")
	switch {
	case comma >= 0 && dot >= 0:
		if comma > dot {
			return strings.ReplaceAll(strings.ReplaceAll(cleaned, ".", ""), ",", ".")
		}
		return strings.ReplaceAll(cleaned, ",", "")
	case comma >= 0:
		return splitLastSeparator(cleaned, ",")
	case dot >= 0:
		return splitLastSeparator(cleaned, ".")
	default:
		return cleaned
	}
}

// splitLastSeparator mirrors Money.split_last_separator.
func splitLastSeparator(cleaned, separator string) string {
	other := ","
	if separator == "," {
		other = "."
	}
	whole, frac, found := strings.Cut(cleaned, separator)
	if !found {
		return cleaned
	}
	if len(frac) <= 2 && !strings.Contains(frac, separator) {
		return strings.ReplaceAll(whole, other, "") + "." + frac
	}
	return strings.ReplaceAll(cleaned, separator, "")
}

var decimalRe = regexp.MustCompile(`^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$`)

// parseDecimal parses a strict decimal (no commas, hex, or fractions).
func parseDecimal(s string) (*big.Rat, bool) {
	if !decimalRe.MatchString(s) {
		return nil, false
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, false
	}
	return r, true
}

// roundHalfUp rounds to an integer, ties toward +∞ (Ruby ROUND_HALF_UP).
func roundHalfUp(r *big.Rat) (int64, error) {
	num := r.Num()
	den := r.Denom()
	q := new(big.Int)
	rem := new(big.Int)
	q.QuoRem(num, den, rem)
	if rem.Sign() == 0 {
		return toInt64(q)
	}
	double := new(big.Int).Abs(rem)
	double.Lsh(double, 1)
	switch {
	case num.Sign() > 0:
		if double.Cmp(den) >= 0 {
			q.Add(q, big.NewInt(1))
		}
	default: // negative: truncate went toward zero, ties stay (toward +∞)
		if double.Cmp(den) > 0 {
			q.Sub(q, big.NewInt(1))
		}
	}
	return toInt64(q)
}

func toInt64(v *big.Int) (int64, error) {
	if !v.IsInt64() {
		return 0, &MissingRate{Currency: "overflow"}
	}
	return v.Int64(), nil
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func pad2(n int64) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return itoa(n)
}
