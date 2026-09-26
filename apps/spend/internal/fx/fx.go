// Package fx fetches live USD/EUR -> BRL quotes from dolarhoje.com and
// overlays them on the manual users.fx rates. Manual values stay untouched
// as the offline fallback; the auto cache is global (one row per currency).
package fx

import (
	"context"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/aquasp/kuraspend/internal/money"
	"github.com/aquasp/kuraspend/internal/store"
)

// Sources for the commercial quotes, in page order (dollar, euro).
const (
	DollarURL = "https://dolarhoje.com"
	EuroURL   = "https://dolarhoje.com/euro-hoje/"
)

// TTL is how long a cached quote counts as fresh. The site updates on
// business days; half a day keeps values current without hammering it.
const TTL = 12 * time.Hour

// Timeout bounds one quote fetch; MaxBody caps the page read (pages are ~60KB).
const (
	Timeout = 10 * time.Second
	MaxBody = 1 << 20
)

// maxRate guards against parsing garbage (a job field, a year) as a quote.
const maxRate = 10000

// DefaultClient fetches quotes with a sane timeout.
func DefaultClient() *http.Client { return &http.Client{Timeout: Timeout} }

var (
	nacionalRe  = regexp.MustCompile(`id="nacional"[^>]*?value="([^"]+)"`)
	nacionalRev = regexp.MustCompile(`value="([^"]+)"[^>]*?id="nacional"`)
)

// ParseRate extracts the commercial quote from a dolarhoje.com page, where
// the converter box renders <input ... id="nacional" value="5,19">. It
// returns the canonical decimal form ("5.19") accepted by money.ValidRate.
func ParseRate(body []byte) (string, error) {
	raw := matchRate(nacionalRe, body)
	if raw == "" {
		raw = matchRate(nacionalRev, body)
	}
	if raw == "" {
		return "", fmt.Errorf("fx: quote box not found")
	}
	rate, ok := normalizeRate(raw)
	if !ok {
		return "", fmt.Errorf("fx: bad quote %q", raw)
	}
	return rate, nil
}

func matchRate(re *regexp.Regexp, body []byte) string {
	if m := re.FindSubmatch(body); len(m) == 2 {
		return string(m[1])
	}
	return ""
}

// normalizeRate turns the site's Brazilian decimal ("5,19", at most
// "1.234,56") into canonical "5.19", rejecting non-positive and absurd
// values.
func normalizeRate(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if strings.Contains(s, ",") {
		s = strings.ReplaceAll(strings.ReplaceAll(s, ".", ""), ",", ".")
	}
	if !money.ValidRate(s) {
		return "", false
	}
	r, _ := new(big.Rat).SetString(s)
	if r == nil || r.Cmp(big.NewRat(maxRate, 1)) >= 0 {
		return "", false
	}
	return r.FloatString(2), true
}

// FetchRate GETs one quote page and parses the commercial rate.
func FetchRate(ctx context.Context, client *http.Client, url string) (string, error) {
	if client == nil {
		client = DefaultClient()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "kuraspend/fx")
	req.Header.Set("Accept", "text/html")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fx: %s status %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody))
	if err != nil {
		return "", err
	}
	return ParseRate(body)
}

// Refresh fetches the USD and EUR quotes and caches each parsed rate.
// Partial success is fine (one row upserted); err is non-nil only when
// nothing refreshed. It returns the count of refreshed currencies.
func Refresh(ctx context.Context, st *store.Store, client *http.Client) (int, error) {
	return RefreshTarget(ctx, st, client,
		[2]target{{"USD", DollarURL}, {"EUR", EuroURL}})
}

type target struct{ code, url string }

// RefreshTarget is Refresh with injectable URLs, so tests point at a local
// server instead of the live site.
func RefreshTarget(ctx context.Context, st *store.Store, client *http.Client, targets [2]target) (int, error) {
	refreshed := 0
	var errs []string
	for _, t := range targets {
		rate, err := FetchRate(ctx, client, t.url)
		if err != nil {
			errs = append(errs, t.code+": "+err.Error())
			continue
		}
		if err := st.UpsertFXRate(t.code, rate); err != nil {
			errs = append(errs, t.code+": "+err.Error())
			continue
		}
		refreshed++
	}
	if refreshed == 0 {
		return 0, fmt.Errorf("fx: %s", strings.Join(errs, "; "))
	}
	return refreshed, nil
}

// Quote is one cached rate for display; Fresh reports TTL freshness.
type Quote struct {
	Rate      string
	FetchedAt time.Time
	Fresh     bool
}

// Quotes returns the cached USD/EUR rows, fresh or stale, for the settings
// UI. Missing rows are simply absent from the map.
func Quotes(st *store.Store) map[string]Quote {
	rows, err := st.FXQuotes()
	if err != nil {
		return map[string]Quote{}
	}
	out := make(map[string]Quote, len(rows))
	for code, q := range rows {
		out[code] = Quote{Rate: q.Rate, FetchedAt: q.FetchedAt,
			Fresh: time.Since(q.FetchedAt) <= TTL}
	}
	return out
}

// EffectiveRates overlays fresh auto quotes on the manual fx hash. The auto
// quotes are X->BRL, so they only apply when home is BRL; otherwise, and
// whenever the cache is stale or empty, the manual values stand alone.
func EffectiveRates(st *store.Store, u *store.User) map[string]string {
	out := store.FXHash(u)
	if strings.ToUpper(u.HomeCurrency) != "BRL" {
		return out
	}
	for code, q := range Quotes(st) {
		if !q.Fresh || !money.ValidCurrency(code) || code == "BRL" {
			continue
		}
		out[code] = q.Rate
	}
	return out
}
