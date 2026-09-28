package mail

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"
	"unicode"
)

func validateOutgoing(msg Outgoing) error {
	if len(cleanAddrs(msg.To)) == 0 {
		return fmt.Errorf("to is required")
	}
	for _, a := range append(append([]string{}, msg.To...), append(msg.Cc, msg.Bcc...)...) {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if _, err := mail.ParseAddress(a); err != nil {
			return fmt.Errorf("bad address %q", a)
		}
	}
	return nil
}

func cleanAddrs(in []string) []string {
	var out []string
	for _, a := range in {
		a = strings.TrimSpace(a)
		if a != "" {
			out = append(out, a)
		}
	}
	return out
}

func recipients(msg Outgoing) []string {
	return append(append(cleanAddrs(msg.To), cleanAddrs(msg.Cc)...), cleanAddrs(msg.Bcc)...)
}

func formatAddress(name, addr string) string {
	addr = strings.TrimSpace(addr)
	name = strings.TrimSpace(name)
	if name == "" {
		return addr
	}
	return (&mail.Address{Name: name, Address: addr}).String()
}

func buildRFC822(c Creds, msg Outgoing) ([]byte, error) {
	if err := validateOutgoing(msg); err != nil {
		return nil, err
	}
	var b bytes.Buffer
	writeHeader(&b, "From", formatAddress(c.DisplayName, c.From))
	writeHeader(&b, "To", strings.Join(cleanAddrs(msg.To), ", "))
	if cc := cleanAddrs(msg.Cc); len(cc) > 0 {
		writeHeader(&b, "Cc", strings.Join(cc, ", "))
	}
	writeHeader(&b, "Subject", mime.QEncoding.Encode("utf-8", msg.Subject))
	writeHeader(&b, "Date", time.Now().UTC().Format(time.RFC1123Z))
	writeHeader(&b, "Message-ID", "<"+newMessageID(c.From)+">")
	if msg.InReplyTo != "" {
		writeHeader(&b, "In-Reply-To", msg.InReplyTo)
	}
	if msg.References != "" {
		writeHeader(&b, "References", msg.References)
	}
	writeHeader(&b, "MIME-Version", "1.0")
	writeHeader(&b, "Content-Type", "text/plain; charset=utf-8")
	writeHeader(&b, "Content-Transfer-Encoding", "8bit")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(strings.ReplaceAll(msg.Body, "\r\n", "\n"), "\n", "\r\n"))
	return b.Bytes(), nil
}

func writeHeader(b *bytes.Buffer, key, value string) {
	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteString("\r\n")
}

func newMessageID(from string) string {
	buf := make([]byte, 12)
	_, _ = rand.Read(buf)
	host := "localhost"
	if i := strings.LastIndex(from, "@"); i >= 0 && i < len(from)-1 {
		host = from[i+1:]
	}
	return hex.EncodeToString(buf) + "@" + host
}

func parseRFC822(raw []byte) (*Message, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	out := &Message{}
	out.From = decodeHeader(msg.Header.Get("From"))
	out.To = decodeHeader(msg.Header.Get("To"))
	out.Cc = decodeHeader(msg.Header.Get("Cc"))
	out.Subject = decodeHeader(msg.Header.Get("Subject"))
	out.MessageID = strings.TrimSpace(msg.Header.Get("Message-Id"))
	out.InReplyTo = strings.TrimSpace(msg.Header.Get("In-Reply-To"))
	out.References = strings.TrimSpace(msg.Header.Get("References"))
	if t, err := mail.ParseDate(msg.Header.Get("Date")); err == nil {
		out.Date = t.UTC()
	}
	body, err := io.ReadAll(io.LimitReader(msg.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	walkMIME(msg.Header, body, out)
	if out.Text == "" && len(out.parts) == 0 {
		out.Text = strings.TrimSpace(string(body))
	}
	return out, nil
}

func decodeHeader(s string) string {
	dec := new(mime.WordDecoder)
	out, err := dec.DecodeHeader(s)
	if err != nil {
		return s
	}
	return out
}

func walkMIME(h mail.Header, body []byte, out *Message) {
	ctype := h.Get("Content-Type")
	if ctype == "" {
		ctype = "text/plain"
	}
	media, params, err := mime.ParseMediaType(ctype)
	if err != nil {
		media = "text/plain"
	}
	cte := h.Get("Content-Transfer-Encoding")
	decoded := decodeCTE(body, cte)
	if strings.HasPrefix(media, "multipart/") {
		mr := multipart.NewReader(bytes.NewReader(decoded), params["boundary"])
		var plain, html string
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			pb, _ := io.ReadAll(io.LimitReader(part, 8<<20))
			pmedia, pparams, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
			pcte := part.Header.Get("Content-Transfer-Encoding")
			pdec := decodeCTE(pb, pcte)
			if strings.HasPrefix(pmedia, "multipart/") {
				ph := mail.Header(part.Header)
				walkMIME(ph, pb, out)
				continue
			}
			disp, dparams, _ := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
			name := dparams["filename"]
			if name == "" {
				name = pparams["name"]
			}
			name = decodeHeader(name)
			if disp == "attachment" || (name != "" && !strings.HasPrefix(pmedia, "text/")) {
				idx := len(out.Attachments)
				out.Attachments = append(out.Attachments, Attachment{
					Index: idx, Name: name, MIME: pmedia, Size: len(pdec),
				})
				out.parts = append(out.parts, pdec)
				continue
			}
			switch {
			case pmedia == "text/plain" || pmedia == "":
				if plain == "" {
					plain = string(pdec)
				}
			case pmedia == "text/html":
				if html == "" {
					html = string(pdec)
				}
			}
		}
		if out.Text == "" {
			if plain != "" {
				out.Text = plain
			} else if html != "" {
				out.Text = htmlToText(html)
			}
		}
		return
	}
	if strings.HasPrefix(media, "text/html") {
		out.Text = htmlToText(string(decoded))
		return
	}
	out.Text = string(decoded)
}

func decodeCTE(body []byte, cte string) []byte {
	switch strings.ToLower(strings.TrimSpace(cte)) {
	case "base64":
		out, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, bytes.NewReader(body)))
		if err != nil {
			return body
		}
		return out
	case "quoted-printable":
		out, err := io.ReadAll(quotedprintable.NewReader(bytes.NewReader(body)))
		if err != nil {
			return body
		}
		return out
	default:
		return body
	}
}

func htmlToText(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
		case in:
			continue
		default:
			if unicode.IsSpace(r) {
				if b.Len() > 0 && !strings.HasSuffix(b.String(), " ") && !strings.HasSuffix(b.String(), "\n") {
					b.WriteByte(' ')
				}
				continue
			}
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// headerOnly parses a header block from FETCH BODY[HEADER.FIELDS ...].
func headerOnly(raw []byte) Header {
	msg, err := mail.ReadMessage(bytes.NewReader(append(raw, '\n')))
	if err != nil {
		return Header{}
	}
	h := Header{
		From:      decodeHeader(msg.Header.Get("From")),
		To:        decodeHeader(msg.Header.Get("To")),
		Subject:   decodeHeader(msg.Header.Get("Subject")),
		MessageID: strings.TrimSpace(msg.Header.Get("Message-Id")),
	}
	if t, err := mail.ParseDate(msg.Header.Get("Date")); err == nil {
		h.Date = t.UTC()
	}
	return h
}

func quoteReply(msg *Message) Outgoing {
	to := msg.From
	if addr, err := mail.ParseAddress(msg.From); err == nil {
		to = addr.Address
	}
	subject := msg.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}
	var quoted strings.Builder
	quoted.WriteString("\n\n")
	for _, line := range strings.Split(msg.Text, "\n") {
		quoted.WriteString("> ")
		quoted.WriteString(strings.TrimRight(line, "\r"))
		quoted.WriteString("\n")
	}
	refs := strings.TrimSpace(msg.References)
	if msg.MessageID != "" {
		if refs != "" {
			refs += " "
		}
		refs += msg.MessageID
	}
	return Outgoing{
		To:         []string{to},
		Subject:    subject,
		Body:       quoted.String(),
		InReplyTo:  msg.MessageID,
		References: refs,
	}
}
