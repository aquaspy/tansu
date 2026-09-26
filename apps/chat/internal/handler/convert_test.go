package handler

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aquasp/kurachat/internal/config"
	"github.com/aquasp/kurachat/internal/store"
)

// fakeSoffice renders <base>.pdf into --outdir like the real converter.
func fakeSoffice(t *testing.T) string {
	t.Helper()
	script := `#!/bin/sh
out=""; in=""
for a in "$@"; do
  if [ "$prev" = "--outdir" ]; then out="$a"; fi
  prev="$a"; in="$a"
done
base=$(basename "$in"); base=${base%.*}
printf '%%PDF-1.4 fake for %s\n' "$base" > "$out/$base.pdf"
`
	p := filepath.Join(t.TempDir(), "soffice")
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func postFile(t *testing.T, f *flow, convID, field, filename string, raw []byte) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("content", "Read this")
	_ = mw.WriteField("csrf_token", f.csrf())
	w, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write(raw)
	_ = mw.Close()
	req, _ := http.NewRequest(http.MethodPost, f.server.URL+"/conversations/"+convID+"/messages", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("HX-Request", "true")
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestDocxUploadConverts(t *testing.T) {
	bin := fakeSoffice(t)
	f := newFlow(t, func(c *config.Config) { c.LibreOfficeBin = bin })
	u := f.seedUser("you@x.com", "secret-ok")
	conv, _ := f.store.CreateConversation(u.ID)
	f.login("you@x.com", "secret-ok")
	// Minimal zip magic: OOXML sniffs as application/zip, routed by ext.
	code, body := postFile(t, f, "1", "files[]", "memo.docx", []byte("PK\x03\x04fake-docx"))
	if code != 200 {
		t.Fatalf("upload = %d %.300s", code, body)
	}
	rows, _ := f.store.Transcript(conv.ID)
	if len(rows) == 0 || len(rows[0].Documents) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	doc := rows[0].Documents[0]
	if doc.Filename != "memo.pdf" || doc.ContentType != "application/pdf" {
		t.Fatalf("doc = %+v", doc)
	}
}

func TestDocUploadRejectsUnknown(t *testing.T) {
	bin := fakeSoffice(t)
	f := newFlow(t, func(c *config.Config) { c.LibreOfficeBin = bin })
	f.seedUser("you@x.com", "secret-ok")
	_, _ = f.store.CreateConversation(1)
	f.login("you@x.com", "secret-ok")
	code, _ := postFile(t, f, "1", "files[]", "archive.zip", []byte("PK\x03\x04nope"))
	if code != 200 {
		t.Fatalf("upload = %d, want 200 with error alert", code)
	}
	rows, _ := f.store.Transcript(1)
	users := 0
	for _, m := range rows {
		if m.Role == store.RoleUser {
			users++
		}
	}
	if users != 0 {
		t.Fatalf("zip stored %d user rows", users)
	}
	if !strings.Contains(mustGet(t, f, "/conversations/1"), "Those files can") {
		t.Fatal("missing bad_file alert")
	}
}
