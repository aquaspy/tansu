package store

import (
	"strings"
	"time"
)

// FXQuote is one cached auto rate (see internal/fx).
type FXQuote struct {
	Rate      string
	FetchedAt time.Time
}

// UpsertFXRate stores a freshly fetched quote.
func (s *Store) UpsertFXRate(code, rate string) error {
	_, err := s.db.Exec(`INSERT INTO fx_rates (currency, rate, fetched_at)
		VALUES (?, ?, ?) ON CONFLICT(currency) DO UPDATE
		SET rate = excluded.rate, fetched_at = excluded.fetched_at`,
		strings.ToUpper(code), rate, now())
	return err
}

// FXQuotes returns the cached auto rates keyed by currency. A corrupt
// fetched_at reads as zero time (stale by construction).
func (s *Store) FXQuotes() (map[string]FXQuote, error) {
	rows, err := s.db.Query(`SELECT currency, rate, fetched_at FROM fx_rates`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]FXQuote{}
	for rows.Next() {
		var code, rate, raw string
		if err := rows.Scan(&code, &rate, &raw); err != nil {
			return nil, err
		}
		fetched, _ := parseTime(raw)
		out[strings.ToUpper(code)] = FXQuote{Rate: rate, FetchedAt: fetched}
	}
	return out, rows.Err()
}
