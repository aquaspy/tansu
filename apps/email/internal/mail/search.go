package mail

import (
	"net/textproto"
	"strings"
	"time"

	"github.com/emersion/go-imap"
)

// searchCriteria turns a box query into an IMAP SEARCH. Empty is ALL.
// from:, subject:, and since:YYYY-MM-DD are those keys. text: and body:
// scan the whole message (slow on large mailboxes). Anything else matches
// From, To, Cc, or Subject, which typical servers can answer without
// reading every body.
func searchCriteria(query string) *imap.SearchCriteria {
	c := imap.NewSearchCriteria()
	q := trimQuery(query)
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
		return headerOr(q)
	case strings.HasPrefix(lower, "text:"), strings.HasPrefix(lower, "body:"):
		raw := strings.TrimSpace(q[strings.IndexByte(q, ':')+1:])
		if raw == "" {
			return c
		}
		c.Text = []string{raw}
	default:
		return headerOr(q)
	}
	return c
}

// headerOr is OR OR OR FROM q TO q CC q SUBJECT q.
func headerOr(q string) *imap.SearchCriteria {
	fromTo := &imap.SearchCriteria{Or: [][2]*imap.SearchCriteria{{headerIs("From", q), headerIs("To", q)}}}
	withCc := &imap.SearchCriteria{Or: [][2]*imap.SearchCriteria{{fromTo, headerIs("Cc", q)}}}
	return &imap.SearchCriteria{Or: [][2]*imap.SearchCriteria{{withCc, headerIs("Subject", q)}}}
}

func headerIs(name, value string) *imap.SearchCriteria {
	c := imap.NewSearchCriteria()
	c.Header = textproto.MIMEHeader{name: {value}}
	return c
}

func trimQuery(q string) string {
	q = strings.TrimSpace(q)
	q = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, q)
	rs := []rune(q)
	if len(rs) > maxQueryRunes {
		q = string(rs[:maxQueryRunes])
	}
	return q
}
