package chat

import (
	"regexp"
	"strings"
)

// Guard detects model degeneration (URL floods, closing mantras) and
// truncates the stream at the fault point.
const (
	shortLine   = 48
	maxURLHits  = 4
	maxLineHits = 4
	tailChars   = 1200
)

var (
	urlRe        = regexp.MustCompile(`https?://[^\s\]\"'<>]+`)
	inlineCodeRe = regexp.MustCompile("``[^`\n]+``|`[^`\n]+`")
	fenceOpenRe  = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")
	mantraRe     = regexp.MustCompile(`(?i)^(?:fim\.?|resposta\.?|\*{0,2}resumo\*{0,2}:?\.?|obrigado\.?|pronto\.?)$`)
)

// Check returns the degeneration reason or "" when the text is fine.
func Check(text string) string {
	urls := urlRe.FindAllString(text, -1)
	tally := map[string]int{}
	for _, u := range urls {
		tally[u]++
		if tally[u] >= maxURLHits {
			return "url_repeat"
		}
	}
	if closingLoop(text) {
		return "closing_loop"
	}
	return ""
}

// Truncate cuts degenerated output at the fault point.
func Truncate(text, reason string) string {
	switch reason {
	case "url_repeat":
		if cut := cutAtURLFlood(text); cut != nil {
			return *cut
		}
		return text
	case "closing_loop":
		return stripTrailingRepeatedLines(text)
	default:
		return text
	}
}

// CodeStash is the opaque placeholder table from ProtectCode.
type CodeStash struct {
	sentinel string
	blocks   []string
}

// ProtectCode stashes fenced blocks and inline spans behind placeholders.
func ProtectCode(text string) (string, CodeStash) {
	sentinel := "\uE001"
	for _, s := range []string{"\uE001", "\uE002", "\uE003"} {
		if !strings.Contains(text, s) {
			sentinel = s
			break
		}
	}
	stash := CodeStash{sentinel: sentinel}
	stashBlock := func(code string) string {
		stash.blocks = append(stash.blocks, code)
		return sentinel + itoa(len(stash.blocks)-1) + sentinel
	}
	lines := strings.SplitAfter(text, "\n")
	var out strings.Builder
	for i := 0; i < len(lines); {
		m := fenceOpenRe.FindStringSubmatch(strings.TrimRight(lines[i], "\n"))
		if m == nil {
			out.WriteString(lines[i])
			i++
			continue
		}
		fence := m[1]
		char, length := fence[0], len(fence)
		closeRe := regexp.MustCompile(`^ {0,3}` + regexp.QuoteMeta(string(char)) +
			`{` + itoa(length) + `,}[ \t]*(?:\n|$)`)
		j := i + 1
		for j < len(lines) && !closeRe.MatchString(lines[j]) {
			j++
		}
		if j < len(lines) {
			j++
		}
		out.WriteString(stashBlock(strings.Join(lines[i:j], "")))
		i = j
	}
	safe := inlineCodeRe.ReplaceAllStringFunc(out.String(), stashBlock)
	return safe, stash
}

// RestoreCode swaps placeholders back for the stashed code.
func RestoreCode(safe string, stash CodeStash) string {
	re := regexp.MustCompile(regexp.QuoteMeta(stash.sentinel) + `(\d+)` + regexp.QuoteMeta(stash.sentinel))
	return re.ReplaceAllStringFunc(safe, func(m string) string {
		inner := strings.Trim(m, stash.sentinel)
		n := atoi(inner)
		if n < 0 || n >= len(stash.blocks) {
			return m
		}
		return stash.blocks[n]
	})
}

func closingLoop(text string) bool {
	r := []rune(text)
	if len(r) > tailChars {
		r = r[len(r)-tailChars:]
	}
	var lines []string
	for _, line := range strings.Split(string(r), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			lines = append(lines, s)
		}
	}
	if len(lines) < 6 {
		return false
	}
	tail := lines
	if len(tail) > 40 {
		tail = tail[len(tail)-40:]
	}
	tally := map[string]int{}
	for _, line := range tail {
		tally[line]++
		if len([]rune(line)) <= shortLine && tally[line] >= maxLineHits {
			return true
		}
	}
	return false
}

func cutAtURLFlood(text string) *string {
	urls := urlRe.FindAllString(text, -1)
	tally := map[string]int{}
	for _, u := range urls {
		tally[u]++
	}
	var offender string
	best := 0
	for u, n := range tally {
		if n > best {
			offender, best = u, n
		}
	}
	if best < maxURLHits {
		return nil
	}
	hits, pos := 0, 0
	for {
		found := strings.Index(text[pos:], offender)
		if found < 0 {
			return nil
		}
		hits++
		if hits >= maxURLHits {
			s := strings.TrimRight(text[:pos+found], " \t\n")
			return &s
		}
		pos += found + len(offender)
	}
}

func stripTrailingRepeatedLines(text string) string {
	lines := strings.SplitAfter(text, "\n")
	for len(lines) >= 2 && (closingLoop(strings.Join(lines, "")) || mantraLine(lines[len(lines)-1])) {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimRight(strings.Join(lines, ""), " \t\n")
}

func mantraLine(line string) bool {
	s := strings.TrimSpace(line)
	if s == "" || len([]rune(s)) > shortLine {
		return false
	}
	return mantraRe.MatchString(s)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func atoi(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return -1
		}
		n = n*10 + int(s[i]-'0')
	}
	return n
}
