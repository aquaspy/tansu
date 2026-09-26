// Package notes holds the note-domain logic shared by the web and API
// handlers: the multi-format importer.
package notes

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"path"
	"strconv"
	"strings"
)

const (
	// MaxUpload mirrors NoteImporter::MAX_UPLOAD (per file).
	MaxUpload = 15 << 20
	// MaxEntry mirrors NoteImporter::MAX_ENTRY (per zip entry).
	MaxEntry = 2 << 20
	// MaxNotes mirrors NoteImporter::MAX_NOTES (per import).
	MaxNotes = 500
)

// Upload is one uploaded file.
type Upload struct {
	Name string
	Data []byte
}

// Row is one parsed note.
type Row struct {
	Body   string
	Folder string
}

// Rows parses uploads into note rows. It mirrors NoteImporter.rows: blank
// results and unreadable input are errors; the output is capped at MaxNotes.
func Rows(uploads []Upload) ([]Row, error) {
	if len(uploads) == 0 {
		return nil, errors.New("missing_file")
	}
	var found []Row
	for _, up := range uploads {
		if len(up.Data) > MaxUpload {
			return nil, errors.New("too_large")
		}
		found = append(found, parseBlob(up.Data, up.Name)...)
	}
	if len(found) == 0 {
		return nil, errors.New("empty")
	}
	if len(found) > MaxNotes {
		found = found[:MaxNotes]
	}
	return found, nil
}

func parseBlob(raw []byte, name string) []Row {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if isZip(raw, name) {
		return parseZip(raw)
	}
	text := decode(raw)
	if v, ok := tryJSON(text); ok {
		return parseJSON(v, name)
	}
	if row, ok := plaintextRow(text, ""); ok {
		return []Row{row}
	}
	return nil
}

func parseJSON(v any, name string) []Row {
	switch t := v.(type) {
	case []any:
		var out []Row
		for _, item := range t {
			if row, ok := kuranotesRow(item); ok {
				out = append(out, row)
			}
		}
		return out
	case map[string]any:
		if items, ok := t["items"].([]any); ok {
			return standardNotesRows(items)
		}
		if list, ok := t["notes"].([]any); ok {
			var out []Row
			for _, item := range list {
				if row, ok := kuranotesRow(item); ok {
					out = append(out, row)
				}
			}
			return out
		}
		if root, ok := t["root"].(map[string]any); ok {
			body := joinNonBlank(titleFromFilename(name), lexicalToText(root))
			if body != "" {
				return []Row{{Body: body}}
			}
		}
	}
	return nil
}

func parseZip(data []byte) []Row {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil
	}
	// A Standard Notes backup inside the zip wins over loose files.
	for _, entry := range zr.File {
		if entry.FileInfo().IsDir() || !isBackupName(entry.Name) {
			continue
		}
		text, err := readEntry(entry)
		if err != nil {
			continue
		}
		if v, ok := tryJSON(text); ok {
			return parseJSON(v, entry.Name)
		}
	}
	var out []Row
	for _, entry := range zr.File {
		if skipZipEntry(entry) {
			continue
		}
		text, err := readEntry(entry)
		if err != nil || strings.TrimSpace(text) == "" {
			continue
		}
		if v, ok := tryJSON(text); ok {
			out = append(out, parseJSON(v, entry.Name)...)
			continue
		}
		if row, ok := plaintextRow(text, folderFromZipPath(entry.Name)); ok {
			out = append(out, row)
		}
	}
	return out
}

func kuranotesRow(item any) (Row, bool) {
	m, ok := item.(map[string]any)
	if !ok {
		return Row{}, false
	}
	body := stringField(m, "body")
	if strings.TrimSpace(body) == "" {
		return Row{}, false
	}
	return Row{Body: body, Folder: stringField(m, "folder")}, true
}

func standardNotesRows(items []any) []Row {
	tagsByNote := map[string]string{}
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok || m["content_type"] != "Tag" {
			continue
		}
		content, ok := m["content"].(map[string]any)
		if !ok {
			continue
		}
		title := strings.TrimSpace(stringField(content, "title"))
		if title == "" {
			continue
		}
		refs, _ := content["references"].([]any)
		for _, ref := range refs {
			uuid := ""
			if rm, ok := ref.(map[string]any); ok {
				uuid, _ = rm["uuid"].(string)
			}
			if uuid != "" {
				if _, seen := tagsByNote[uuid]; !seen {
					tagsByNote[uuid] = title
				}
			}
		}
	}
	var out []Row
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok || m["content_type"] != "Note" {
			continue
		}
		if deleted, _ := m["deleted"].(bool); deleted {
			continue
		}
		content, ok := m["content"].(map[string]any)
		if !ok {
			continue
		}
		body := joinNonBlank(
			strings.TrimSpace(stringField(content, "title")),
			noteText(content),
		)
		if body == "" {
			continue
		}
		uuid, _ := m["uuid"].(string)
		out = append(out, Row{Body: body, Folder: tagsByNote[uuid]})
	}
	return out
}

func noteText(content map[string]any) string {
	raw := stringField(content, "text")
	if v, ok := tryJSON(raw); ok {
		if m, ok := v.(map[string]any); ok {
			if root, ok := m["root"].(map[string]any); ok {
				return lexicalToText(root)
			}
		}
	}
	return raw
}

func plaintextRow(text, folder string) (Row, bool) {
	body := strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if body == "" {
		return Row{}, false
	}
	return Row{Body: body, Folder: folder}, true
}

// lexicalToText mirrors NoteImporter#lexical_to_text.
func lexicalToText(node map[string]any) string {
	return lexNode(node, "", 0)
}

func lexChildren(node map[string]any) []map[string]any {
	raw, _ := node["children"].([]any)
	var out []map[string]any
	for _, c := range raw {
		if m, ok := c.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func lexNode(node map[string]any, listType string, index int) string {
	typ, _ := node["type"].(string)
	children := lexChildren(node)
	walk := func() string {
		var b strings.Builder
		for _, c := range children {
			b.WriteString(lexNode(c, listType, index))
		}
		return b.String()
	}
	switch typ {
	case "text":
		s, _ := node["text"].(string)
		return s
	case "linebreak", "tab":
		return "\n"
	case "heading", "paragraph":
		return strings.TrimSpace(walk()) + "\n\n"
	case "quote":
		var b strings.Builder
		for _, line := range strings.Split(strings.TrimSpace(walk()), "\n") {
			b.WriteString("> " + line + "\n")
		}
		return b.String() + "\n"
	case "horizontalrule":
		return "---\n\n"
	case "list":
		kind := stringField(node, "listType")
		if kind == "" {
			kind = stringField(node, "tag")
		}
		var b strings.Builder
		for i, c := range children {
			b.WriteString(lexNode(c, kind, i+1))
		}
		return b.String() + "\n"
	case "listitem":
		inner := strings.TrimSpace(walk())
		prefix := "- "
		if checked, ok := node["checked"].(bool); ok {
			if checked {
				prefix = "[x] "
			} else {
				prefix = "[ ] "
			}
		} else {
			switch strings.ToLower(listType) {
			case "number", "numbered", "ol":
				prefix = strconv.Itoa(index) + ". "
			}
		}
		return prefix + inner + "\n"
	case "root":
		return collapseBlankLines(strings.TrimSpace(walk()))
	default:
		return walk()
	}
}

func collapseBlankLines(s string) string {
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return s
}

func titleFromFilename(name string) string {
	base := path.Base(strings.ReplaceAll(name, `\`, "/"))
	ext := path.Ext(base)
	base = strings.TrimSuffix(base, ext)
	// Strip a trailing -<8 hex> Notesnook-style suffix.
	if len(base) > 9 && base[len(base)-9] == '-' && isHex(base[len(base)-8:]) {
		base = base[:len(base)-9]
	}
	return strings.TrimSpace(strings.ReplaceAll(base, "-", " "))
}

func isHex(s string) bool {
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

func folderFromZipPath(name string) string {
	parts := strings.Split(strings.ReplaceAll(name, `\`, "/"), "/")
	parts = parts[:len(parts)-1] // drop the filename
	var kept []string
	for _, part := range parts {
		if part == "" || part == "." || part == "Items" || part == "Note" ||
			strings.HasPrefix(part, "__") {
			continue
		}
		kept = append(kept, part)
	}
	if len(kept) == 0 {
		return ""
	}
	return kept[len(kept)-1]
}

func skipZipEntry(entry *zip.File) bool {
	if entry.FileInfo().IsDir() {
		return true
	}
	p := strings.ReplaceAll(entry.Name, `\`, "/")
	if strings.Contains(p, "..") || strings.HasPrefix(p, "/") {
		return true
	}
	if strings.Contains(p, "__MACOSX") || strings.HasPrefix(path.Base(p), ".") {
		return true
	}
	lower := strings.ToLower(p)
	if !(strings.HasSuffix(lower, ".txt") || strings.HasSuffix(lower, ".json") ||
		strings.HasSuffix(lower, ".md")) {
		return true
	}
	return entry.UncompressedSize64 > MaxEntry
}

func isBackupName(name string) bool {
	return strings.Contains(strings.ToLower(path.Base(name)), "backup and import file")
}

func isZip(data []byte, name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".zip") || bytes.HasPrefix(data, []byte("PK"))
}

func tryJSON(text string) (any, bool) {
	trimmed := strings.TrimLeft(strings.TrimPrefix(text, "\uFEFF"), " \t\r\n")
	if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
		return nil, false
	}
	var v any
	if err := json.Unmarshal([]byte(trimmed), &v); err != nil {
		return nil, false
	}
	return v, true
}

func readEntry(entry *zip.File) (string, error) {
	rc, err := entry.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(rc); err != nil {
		return "", err
	}
	return decode(buf.Bytes()), nil
}

// decode mirrors the Rails importer: valid UTF-8, no NULs, LF endings.
func decode(data []byte) string {
	s := strings.ToValidUTF8(string(data), "")
	s = strings.ReplaceAll(s, "\x00", "")
	return strings.ReplaceAll(s, "\r\n", "\n")
}

func stringField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func joinNonBlank(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "\n\n")
}
