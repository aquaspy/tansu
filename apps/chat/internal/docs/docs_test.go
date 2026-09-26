package docs

import (
	"bytes"
	"mime/multipart"
	"os"
	"path/filepath"
	"testing"
)

var tinyPDF = []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\ntrailer\n<<>>\n")

func pdfHeader(t *testing.T, name string, raw []byte) *multipart.FileHeader {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	w, err := mw.CreateFormFile("files[]", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	form, err := multipart.NewReader(&buf, mw.Boundary()).ReadForm(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	return form.File["files[]"][0]
}

func TestSavePDF(t *testing.T) {
	dir := t.TempDir()
	sv, err := Save(dir, pdfHeader(t, "report.pdf", tinyPDF))
	if err != nil {
		t.Fatal(err)
	}
	if sv.ContentType != PDFMime || sv.ByteSize != int64(len(tinyPDF)) || sv.Filename != "report.pdf" {
		t.Fatalf("saved = %+v", sv)
	}
	raw, err := os.ReadFile(OriginalPath(dir, sv.SHA256))
	if err != nil || !bytes.Equal(raw, tinyPDF) {
		t.Fatalf("stored bytes err=%v", err)
	}
}

func TestSaveRejectsNonPDF(t *testing.T) {
	dir := t.TempDir()
	if _, err := Save(dir, pdfHeader(t, "note.txt", []byte("hello"))); err == nil {
		t.Fatal("expected rejection")
	}
}

func TestSaveRejectsOversize(t *testing.T) {
	dir := t.TempDir()
	big := append([]byte("%PDF-1.4\n"), bytes.Repeat([]byte("0"), MaxBytes)...)
	if _, err := Save(dir, pdfHeader(t, "big.pdf", big)); err == nil {
		t.Fatal("expected rejection")
	}
}

func TestPurge(t *testing.T) {
	dir := t.TempDir()
	sv, err := Save(dir, pdfHeader(t, "report.pdf", tinyPDF))
	if err != nil {
		t.Fatal(err)
	}
	Purge(dir, sv.SHA256)
	matches, _ := filepath.Glob(filepath.Join(dir, "uploads", sv.SHA256[:2], sv.SHA256+".*"))
	if len(matches) != 0 {
		t.Fatalf("leftovers = %v", matches)
	}
	if !ValidateContentType("application/pdf") || ValidateContentType("image/png") {
		t.Fatal("ValidateContentType")
	}
}
