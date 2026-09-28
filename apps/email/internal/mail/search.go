package mail

import (
	"net/textproto"
	"strings"
	"time"

	"github.com/emersion/go-imap"
)

// searchCriteria turns a box query into an IMAP SEARCH. Empty is ALL.
// from:, subject:, and since:YYYY-MM-DD are sent as those keys; anything
// else is TEXT so the server does the work.
func searchCriteria(query string) *imap.SearchCriteria {
	c := imap.NewSearchCriteria()
	q := strings.TrimSpace(query)
	if q == "" {
		return c
	}
	lower := strings.ToLower(q)
	switch {
	case strings.HasPrefix(lower, "from:"):
		c.Header = textproto.MIMEHeader{"From": {strings.TrimSpace(q[len("from:"):])}}
	case strings.HasPrefix(lower, "subject:"):
		c.Header = textproto.MIMEHeader{"Subject": {strings.TrimSpace(q[len("subject:"):])}}
	case strings.HasPrefix(lower, "since:"):
		raw := strings.TrimSpace(q[len("since:"):])
		if t, err := time.Parse("2006-01-02", raw); err == nil {
			c.Since = t
			return c
		}
		c.Text = []string{q}
	default:
		c.Text = []string{q}
	}
	return c
}
