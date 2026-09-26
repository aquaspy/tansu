package notes

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func TestKuranotesJSON(t *testing.T) {
	rows, err := Rows([]Upload{{
		Name: "kuranotes-2026-01-01.json",
		Data: []byte(`[{"title":"A","body":"A\nfirst","folder":"work","updated_at":"2026-01-01T00:00:00Z"},{"body":"  ","folder":"x"},{"body":"B","folder":""}]`),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Body != "A\nfirst" || rows[0].Folder != "work" {
		t.Fatalf("%+v", rows)
	}
	// {"notes": [...]} wrapper.
	rows, err = Rows([]Upload{{Name: "w.json",
		Data: []byte(`{"notes":[{"body":"wrapped","folder":"z"}]}`)}})
	if err != nil || len(rows) != 1 || rows[0].Folder != "z" {
		t.Fatalf("%+v %v", rows, err)
	}
}

func TestPlaintext(t *testing.T) {
	rows, err := Rows([]Upload{{Name: "note.txt", Data: []byte("hello\r\nworld\x00")}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Body != "hello\nworld" {
		t.Fatalf("%+v", rows)
	}
}

func TestNotesnookZip(t *testing.T) {
	rows, err := Rows([]Upload{{Name: "export.zip", Data: zipData(t, map[string]string{
		"Work/My note-abc12345.txt": "note body",
		"loose.md":                  "# loose",
		"skip.pdf":                  "binary",
		"__MACOSX/x.txt":            "junk",
		".hidden.txt":               "junk",
	})}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("%+v", rows)
	}
	byBody := map[string]string{}
	for _, r := range rows {
		byBody[r.Body] = r.Folder
	}
	if byBody["note body"] != "Work" || byBody["# loose"] != "" {
		t.Fatalf("%+v", rows)
	}
}

func TestStandardNotesBackup(t *testing.T) {
	backup := `{"items":[
		{"uuid":"t1","content_type":"Tag","content":{"title":"travel","references":[{"uuid":"n1"}]}},
		{"uuid":"n1","content_type":"Note","content":{"title":"Trip","text":"Pack light"}},
		{"uuid":"n2","content_type":"Note","deleted":true,"content":{"title":"Gone","text":"x"}},
		{"uuid":"n3","content_type":"Note","content":{"title":"Lex","text":"{\"root\":{\"type\":\"root\",\"children\":[{\"type\":\"paragraph\",\"children\":[{\"type\":\"text\",\"text\":\"hi\"}]}]}}"}},
		{"uuid":"n4","content_type":"Note","content":{"title":"","text":""}}
	]}`
	rows, err := Rows([]Upload{{Name: "Standard Notes Backup and Import File.txt", Data: []byte(backup)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("%+v", rows)
	}
	if rows[0].Body != "Trip\n\nPack light" || rows[0].Folder != "travel" {
		t.Fatalf("tagged: %+v", rows[0])
	}
	if rows[1].Body != "Lex\n\nhi" {
		t.Fatalf("lexical: %+v", rows[1])
	}
	// The same backup nested in a zip wins over loose files.
	rows, err = Rows([]Upload{{Name: "sn.zip", Data: zipData(t, map[string]string{
		"Standard Notes Backup and Import File.txt": backup,
		"loose.txt": "ignored",
	})}})
	if err != nil || len(rows) != 2 {
		t.Fatalf("zip backup: %+v %v", rows, err)
	}
}

func TestLexicalDocument(t *testing.T) {
	doc := `{"root":{"type":"root","children":[
		{"type":"heading","children":[{"type":"text","text":"Title"}]},
		{"type":"list","listType":"number","children":[
			{"type":"listitem","children":[{"type":"text","text":"one"}]},
			{"type":"listitem","checked":true,"children":[{"type":"text","text":"two"}]}
		]},
		{"type":"quote","children":[{"type":"text","text":"wise"}]},
		{"type":"horizontalrule"}
	]}}`
	rows, err := Rows([]Upload{{Name: "My doc-abcdef12.json", Data: []byte(doc)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("%+v", rows)
	}
	want := "My doc\n\nTitle\n\n1. one\n[x] two\n\n> wise\n\n---"
	if rows[0].Body != want {
		t.Fatalf("got %q", rows[0].Body)
	}
}

func TestImportErrors(t *testing.T) {
	if _, err := Rows(nil); err == nil {
		t.Fatal("missing_file accepted")
	}
	if _, err := Rows([]Upload{{Name: "e.txt", Data: []byte("   ")}}); err == nil {
		t.Fatal("blank accepted")
	}
	if _, err := Rows([]Upload{{Name: "big.bin", Data: make([]byte, MaxUpload+1)}}); err == nil {
		t.Fatal("oversize accepted")
	}
	// Unknown JSON shapes import nothing.
	if _, err := Rows([]Upload{{Name: "x.json", Data: []byte(`{"nope":1}`)}}); err == nil {
		t.Fatal("unknown shape accepted")
	}
	// Traversal entries are skipped, not followed.
	rows, err := Rows([]Upload{{Name: "evil.zip", Data: zipData(t, map[string]string{
		"../evil.txt": "x",
		"ok.txt":      "fine",
	})}})
	if err != nil || len(rows) != 1 || rows[0].Body != "fine" {
		t.Fatalf("%+v %v", rows, err)
	}
}

func TestMaxNotesCap(t *testing.T) {
	var parts []string
	for i := 0; i < MaxNotes+50; i++ {
		parts = append(parts, `{"body":"n"}`)
	}
	rows, err := Rows([]Upload{{Name: "many.json",
		Data: []byte("[" + strings.Join(parts, ",") + "]")}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != MaxNotes {
		t.Fatalf("cap: %d", len(rows))
	}
}

// zipData builds an in-memory zip for tests.
func zipData(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	// Deterministic order keeps folder assertions stable.
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	for _, name := range names {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(files[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
