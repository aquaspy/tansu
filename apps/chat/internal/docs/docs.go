// Package docs stores chat document uploads (PDF) on disk.
// Layout under the data dir:
//
//	uploads/ab/abcdef....pdf   original bytes
//
// Unlike images, documents have no derivatives: the original PDF goes
// to the model, which OpenRouter parses via the file-parser plugin.
package docs

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	MaxBytes = 8 << 20 // 8 MB, same as images
	PDFMime  = "application/pdf"
)

func dir(dataDir string) string { return filepath.Join(dataDir, "uploads") }

// OriginalPath is where the raw upload lives.
func OriginalPath(dataDir, sha string) string {
	return filepath.Join(dir(dataDir), sha[:2], sha+".pdf")
}

// Saved describes one stored upload.
type Saved struct {
	Filename    string
	ContentType string
	ByteSize    int64
	SHA256      string
}

// Save validates and stores one upload.
func Save(dataDir string, fh *multipart.FileHeader) (*Saved, error) {
	if fh.Size > MaxBytes {
		return nil, errors.New("too large")
	}
	f, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	return SaveBytes(dataDir, fh.Filename, raw)
}

// SaveBytes validates and stores raw PDF bytes (converted office docs).
func SaveBytes(dataDir, filename string, raw []byte) (*Saved, error) {
	if len(raw) > MaxBytes {
		return nil, errors.New("too large")
	}
	if mime := http.DetectContentType(raw); mime != PDFMime {
		return nil, fmt.Errorf("type %s not allowed", mime)
	}
	sum := fmt.Sprintf("%x", sha256.Sum256(raw))
	sub := filepath.Join(dir(dataDir), sum[:2])
	if err := os.MkdirAll(sub, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(OriginalPath(dataDir, sum), raw, 0o644); err != nil {
		return nil, err
	}
	return &Saved{Filename: filename, ContentType: PDFMime, ByteSize: int64(len(raw)), SHA256: sum}, nil
}

// Purge removes the stored file for sha.
func Purge(dataDir, sha string) {
	if len(sha) < 2 {
		return
	}
	_ = os.Remove(OriginalPath(dataDir, sha))
}

// ValidateContentType reports whether an upload content type is accepted.
func ValidateContentType(mime string) bool {
	return strings.ToLower(strings.TrimSpace(mime)) == PDFMime
}
