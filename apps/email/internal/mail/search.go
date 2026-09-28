package mail

import (
	"fmt"
	"net/textproto"
	"strings"
	"time"

	"github.com/emersion/go-imap"
)

// searchCriteria turns a box query into an IMAP SEARCH. Empty is ALL.
// from:, subject:, and since:YYYY-MM-DD are those keys. text: and body:
// scan message text; listSearch limits that scan to the newest messages
// because a full-mailbox TEXT does not finish on Dovecot. Anything else
// matches From, To, Cc, or Subject, which Dovecot answers from headers.
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

// limitBodyScan restricts a text or body search to UIDs at or above start
// (through *). Header searches are left alone. start <= 1 means the window
// already covers every UID, so the criteria stay unbounded.
func limitBodyScan(c *imap.SearchCriteria, start uint32) bool {
	if c == nil || start <= 1 || (len(c.Text) == 0 && len(c.Body) == 0) {
		return false
	}
	set := new(imap.SeqSet)
	set.AddRange(start, 0) // 0 is "*"
	c.Uid = set
	return true
}

func searchCharset(c *imap.SearchCriteria) string {
	if criteriaASCII(c) {
		return ""
	}
	return "UTF-8"
}

func criteriaASCII(c *imap.SearchCriteria) bool {
	if c == nil {
		return true
	}
	for _, values := range c.Header {
		for _, v := range values {
			if !asciiText(v) {
				return false
			}
		}
	}
	for _, v := range c.Text {
		if !asciiText(v) {
			return false
		}
	}
	for _, v := range c.Body {
		if !asciiText(v) {
			return false
		}
	}
	for _, not := range c.Not {
		if !criteriaASCII(not) {
			return false
		}
	}
	for _, pair := range c.Or {
		if !criteriaASCII(pair[0]) || !criteriaASCII(pair[1]) {
			return false
		}
	}
	return true
}

func asciiText(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}

// newestUIDs walks a sequence set from the high end and keeps at most limit
// UIDs. more is true when the set holds further UIDs. A huge range such as
// 1:10000000 does not get expanded.
func newestUIDs(set *imap.SeqSet, limit int) (uids []uint32, more bool) {
	if set == nil || limit < 1 {
		return nil, false
	}
	for i := len(set.Set) - 1; i >= 0; i-- {
		seq := set.Set[i]
		hi, lo := seq.Stop, seq.Start
		if hi == 0 || lo == 0 {
			return uids, true
		}
		for u := hi; ; u-- {
			if len(uids) == limit {
				return uids, true
			}
			uids = append(uids, u)
			if u == lo {
				break
			}
		}
	}
	return uids, false
}

func seqSetFromIDs(fields []interface{}) (*imap.SeqSet, error) {
	set := new(imap.SeqSet)
	for _, f := range fields {
		n, err := imap.ParseNumber(f)
		if err != nil {
			return nil, err
		}
		set.AddNum(n)
	}
	return set, nil
}

// seqSetFromEsearch reads RFC 4731 ESEARCH return data. A missing ALL means
// the server reported no matches.
func seqSetFromEsearch(fields []interface{}) (*imap.SeqSet, error) {
	i := 0
	if i < len(fields) {
		if _, ok := fields[i].([]interface{}); ok {
			i++
		}
	}
	if i < len(fields) {
		if s, ok := fields[i].(string); ok && strings.EqualFold(s, "UID") {
			i++
		}
	}
	for i < len(fields) {
		key, ok := fields[i].(string)
		if !ok {
			return nil, fmt.Errorf("imap: bad ESEARCH response")
		}
		i++
		if strings.EqualFold(key, "ALL") {
			if i >= len(fields) {
				return nil, fmt.Errorf("imap: ESEARCH ALL missing a sequence set")
			}
			raw, ok := fields[i].(string)
			if !ok {
				return nil, fmt.Errorf("imap: ESEARCH ALL is not a sequence set")
			}
			if raw == "" {
				return new(imap.SeqSet), nil
			}
			return imap.ParseSeqSet(raw)
		}
		if i < len(fields) {
			i++
		}
	}
	return new(imap.SeqSet), nil
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
