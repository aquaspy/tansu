package docs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeSoffice writes a shell stub mimicking `soffice --convert-to`: it
// renders <base>.pdf into --outdir. FAKE_SOFFICE=fail exits 1;
// FAKE_SOFFICE=sleep hangs past any test timeout.
func fakeSoffice(t *testing.T) string {
	t.Helper()
	script := `#!/bin/sh
if [ "$FAKE_SOFFICE" = fail ]; then echo "boom" >&2; exit 1; fi
if [ "$FAKE_SOFFICE" = sleep ]; then sleep 2; fi
out=""; in=""
prev=""
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

func TestConvertible(t *testing.T) {
	for _, f := range []string{"a.docx", "B.PPTX", "dir/slides.pptx"} {
		if !Convertible(f) {
			t.Errorf("%s should convert", f)
		}
	}
	for _, f := range []string{"a.pdf", "a.doc", "a.ppt", "a.zip", "nodot"} {
		if Convertible(f) {
			t.Errorf("%s should not convert", f)
		}
	}
	if got := ConvertedName("dir/Relatório Final.PPTX"); got != "Relatório Final.pdf" {
		t.Errorf("name = %q", got)
	}
}

func TestConvertToPDF(t *testing.T) {
	pdf, err := ConvertToPDF(context.Background(), fakeSoffice(t), "memo.docx", []byte("PKfake-docx"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(pdf), "%PDF-1.4 fake for memo") {
		t.Fatalf("pdf = %q", pdf)
	}
}

func TestConvertToPDFFailure(t *testing.T) {
	t.Setenv("FAKE_SOFFICE", "fail")
	_, err := ConvertToPDF(context.Background(), fakeSoffice(t), "memo.docx", []byte("PKx"))
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
}

func TestConvertToPDFTimeout(t *testing.T) {
	t.Setenv("FAKE_SOFFICE", "sleep")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := ConvertToPDF(ctx, fakeSoffice(t), "memo.docx", []byte("PKx"))
	if err == nil {
		t.Fatal("expected timeout")
	}
}

func TestConvertToPDFRejectsEmpty(t *testing.T) {
	if _, err := ConvertToPDF(context.Background(), fakeSoffice(t), "memo.docx", nil); err == nil {
		t.Fatal("expected error for empty input")
	}
}
