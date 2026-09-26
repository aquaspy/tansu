package chat

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// Render ports MarkdownRenderer.render: structural repairs, CommonMark,
// then the tag/attribute allowlist.
func Render(markdown string) string {
	safe, stash := ProtectCode(markdown)
	fixed := headingSpaceRe.ReplaceAllString(safe, "$1 $2")
	fixed = unglueHeadingBlocks(fixed)
	fixed = unglueMidlineLists(fixed)
	var buf bytes.Buffer
	if err := md().Convert([]byte(RestoreCode(fixed, stash)), &buf); err != nil {
		return ""
	}
	out := policy().Sanitize(buf.String())
	// bluemonday marks absolute links rel="nofollow[ noopener]"; widen to
	// the Rails scrubber's exact rel set.
	out = strings.ReplaceAll(out, ` rel="nofollow noopener"`, ` rel="noopener noreferrer nofollow"`)
	out = strings.ReplaceAll(out, ` rel="nofollow"`, ` rel="noopener noreferrer nofollow"`)
	return out
}

var (
	headingSpaceRe  = regexp.MustCompile(`(?m)^(#{1,6})([^#\s])`)
	gluedTableRe    = regexp.MustCompile(`^(#{1,6}\s+\S.*?)(\|.*\|\s*)$`)
	tableDelimRe    = regexp.MustCompile(`^\s*\|[\s:|\-]*-[\s:|\-]*\|?\s*$`)
	gluedListRe     = regexp.MustCompile(`^(#{1,6}\s+\S.*?\S)([-*+]\s+\S.*|\d{1,3}\.\s+\S.*)\s*$`)
	unorderedItemRe = regexp.MustCompile(`^\s*[-*+]\s+\S`)
	orderedItemRe   = regexp.MustCompile(`^\s*\d{1,3}\.\s+\S`)
	glueMarkerRe    = regexp.MustCompile(`^(?:\d{1,3}\.|[-*+])`)
)

var mdParser = goldmark.New(
	goldmark.WithExtensions(
		extension.NewTable(extension.WithTableCellAlignMethod(extension.TableCellAlignAttribute)),
		extension.Strikethrough,
		extension.Linkify,
	),
)

func md() goldmark.Markdown { return mdParser }

var sanitizePolicy = func() *bluemonday.Policy {
	tags := []string{"p", "br", "strong", "em", "a", "code", "pre", "blockquote",
		"ul", "ol", "li", "h1", "h2", "h3", "h4", "h5", "h6", "hr",
		"table", "thead", "tbody", "tr", "th", "td", "del"}
	p := bluemonday.NewPolicy()
	p.AllowElements(tags...)
	p.AllowAttrs("href", "class", "align", "lang", "title").OnElements(tags...)
	p.RequireParseableURLs(true)
	p.AllowRelativeURLs(true)
	p.AllowURLSchemes("mailto", "http", "https")
	p.AddTargetBlankToFullyQualifiedLinks(true)
	p.RequireNoFollowOnFullyQualifiedLinks(true)
	return p
}()

func policy() *bluemonday.Policy { return sanitizePolicy }

func unglueHeadingBlocks(text string) string {
	lines := strings.SplitAfter(text, "\n")
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		body := strings.TrimRight(line, "\n")
		var next string
		if i+1 < len(lines) {
			next = lines[i+1]
		}
		if m := gluedTableRe.FindStringSubmatch(body); m != nil && tableDelimRe.MatchString(next) {
			out = append(out, strings.TrimRight(m[1], " \t")+"\n", strings.TrimSpace(m[2])+"\n")
			continue
		}
		if m := gluedListRe.FindStringSubmatch(body); m != nil &&
			listConfirmed(m[2], next) && !markerContaminated(m[1], m[2]) {
			out = append(out, strings.TrimRight(m[1], " \t")+"\n", strings.TrimSpace(m[2])+"\n")
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "")
}

func listConfirmed(glued, next string) bool {
	if next == "" {
		return false
	}
	if regexp.MustCompile(`^\d{1,3}\.`).MatchString(glued) {
		return orderedItemRe.MatchString(next)
	}
	return unorderedItemRe.MatchString(next)
}

func markerContaminated(head, glued string) bool {
	marker := glueMarkerRe.FindString(glued)
	if marker == "" {
		return true
	}
	var spaced *regexp.Regexp
	if regexp.MustCompile(`\d`).MatchString(marker) {
		spaced = regexp.MustCompile(`\d{1,3}\.\s`)
	} else {
		spaced = regexp.MustCompile(` ` + regexp.QuoteMeta(marker) + ` `)
	}
	stripped := strings.TrimSpace(strings.TrimPrefix(glued, marker))
	return spaced.MatchString(head) || spaced.MatchString(stripped)
}

// unglueMidlineLists splits collapsed lists ("embarque:- a- b"). RE2 has no
// lookbehind, so the boundary scan is manual: a marker preceded by boundary
// punctuation and followed by spaces + non-space.
func unglueMidlineLists(text string) string {
	var out strings.Builder
	for _, line := range strings.SplitAfter(text, "\n") {
		body := strings.TrimRight(line, "\n")
		nl := line[len(body):]
		if strings.Contains(body, "|") || strings.HasPrefix(strings.TrimLeft(body, " \t"), ">") {
			out.WriteString(line)
			continue
		}
		split := midlineSplitMarker(body)
		if split == 0 {
			out.WriteString(line)
			continue
		}
		out.WriteString(splitMidline(body, split) + nl)
	}
	return out.String()
}

func isBoundary(r rune) bool {
	switch r {
	case ':', ';', '.', ')', ']', '!', '?':
		return true
	}
	return false
}

func isMarker(r rune) bool { return r == '-' || r == '*' || r == '+' }

// midlineHits returns the marker runes that sit on a glue boundary.
func midlineHits(body string) []rune {
	runes := []rune(body)
	var hits []rune
	for i, r := range runes {
		if !isMarker(r) || i == 0 {
			continue
		}
		if !isBoundary(runes[i-1]) {
			continue
		}
		j := i + 1
		spaces := 0
		for j < len(runes) && (runes[j] == ' ' || runes[j] == '\t') {
			spaces++
			j++
		}
		if spaces > 0 && j < len(runes) && runes[j] != ' ' && runes[j] != '\t' && runes[j] != '\n' {
			hits = append(hits, r)
		}
	}
	return hits
}

func midlineSplitMarker(body string) rune {
	tally := map[rune]int{}
	for _, r := range midlineHits(body) {
		tally[r]++
	}
	for r, n := range tally {
		if n >= 2 {
			return r
		}
	}
	return 0
}

func splitMidline(body string, marker rune) string {
	runes := []rune(body)
	var out strings.Builder
	for i, r := range runes {
		if r == marker && i > 0 && isBoundary(runes[i-1]) {
			j := i + 1
			spaces := 0
			for j < len(runes) && (runes[j] == ' ' || runes[j] == '\t') {
				spaces++
				j++
			}
			if spaces > 0 && j < len(runes) && runes[j] != ' ' && runes[j] != '\t' {
				out.WriteByte('\n')
			}
		}
		out.WriteRune(r)
	}
	return out.String()
}
