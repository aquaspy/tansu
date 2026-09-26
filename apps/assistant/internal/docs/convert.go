package docs

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ConvertTimeout bounds one office→PDF conversion (LibreOffice cold start
// plus the convert itself).
const ConvertTimeout = 90 * time.Second

// Convertible reports whether filename is an office format we convert to
// PDF locally. Detection is by extension: OOXML files sniff as plain zip,
// so content sniffing cannot route them.
func Convertible(filename string) bool {
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), ".")) {
	case "docx", "pptx":
		return true
	}
	return false
}

// ConvertedName renames an office upload for its converted PDF.
func ConvertedName(filename string) string {
	base := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	if base == "" {
		base = "document"
	}
	return base + ".pdf"
}

// ConvertToPDF renders an office document to PDF via headless LibreOffice.
// Each run gets an isolated profile directory so concurrent conversions
// never share LO state; the whole work dir is purged afterwards. Output
// must be a real PDF within MaxBytes (same cap as direct PDF uploads).
func ConvertToPDF(ctx context.Context, bin, filename string, raw []byte) ([]byte, error) {
	if bin == "" {
		bin = "soffice"
	}
	if len(raw) == 0 || len(raw) > MaxBytes {
		return nil, fmt.Errorf("input size %d out of range", len(raw))
	}
	work, err := os.MkdirTemp("", "kura-convert-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	safe := filepath.Base(filename)
	if safe == "" || safe == "." {
		safe = "upload" + filepath.Ext(filename)
	}
	in := filepath.Join(work, safe)
	if err := os.WriteFile(in, raw, 0o600); err != nil {
		return nil, err
	}
	out := filepath.Join(work, "out")
	if err := os.MkdirAll(out, 0o700); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, bin,
		"--headless", "--norestore", "--nolockcheck",
		"-env:UserInstallation=file://"+filepath.Join(work, "profile"),
		"--convert-to", "pdf", "--outdir", out, in)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("convert failed: %v: %s", err, firstLine(stderr.String()))
	}
	base := strings.TrimSuffix(safe, filepath.Ext(safe))
	pdf, err := os.ReadFile(filepath.Join(out, base+".pdf"))
	if err != nil || len(pdf) == 0 {
		return nil, fmt.Errorf("no PDF produced")
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF")) {
		return nil, fmt.Errorf("converter output is not a PDF")
	}
	if len(pdf) > MaxBytes {
		return nil, fmt.Errorf("converted PDF too large")
	}
	return pdf, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
