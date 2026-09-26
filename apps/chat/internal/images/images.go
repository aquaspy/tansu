// Package images stores chat uploads on disk and builds derivatives.
// Layout under the data dir:
//
//	uploads/ab/abcdef... .orig        original bytes
//	uploads/ab/abcdef... .thumb.jpg   720px UI thumbnail
//	uploads/ab/abcdef... .model.jpg   1568px JPEG for the model
package images

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp"
)

const (
	// MaxImages caps total attachments per message (images + documents).
	MaxImages   = 4
	MaxBytes    = 8 << 20 // 8 MB
	ThumbLimit  = 720
	ModelLimit  = 1568
	ImageTokens = 2000 // window estimate per attached image
)

var allowedTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
	"image/gif":  true,
}

func dir(dataDir string) string { return filepath.Join(dataDir, "uploads") }

// OriginalPath is where the raw upload lives. ext keeps the source
// extension for operators browsing the volume.
func OriginalPath(dataDir, sha, ext string) string {
	return filepath.Join(dir(dataDir), sha[:2], sha+ext)
}

// ThumbPath serves the UI thumbnail; ModelPath feeds the model.
func ThumbPath(dataDir, sha string) string {
	return filepath.Join(dir(dataDir), sha[:2], sha+".thumb.jpg")
}

func ModelPath(dataDir, sha string) string {
	return filepath.Join(dir(dataDir), sha[:2], sha+".model.jpg")
}

// Saved describes one stored upload.
type Saved struct {
	Filename    string
	ContentType string
	ByteSize    int64
	SHA256      string
	Ext         string
}

// Save validates, stores, and derives one upload.
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
	if len(raw) > MaxBytes {
		return nil, errors.New("too large")
	}
	mime := http.DetectContentType(raw)
	if !allowedTypes[mime] {
		return nil, fmt.Errorf("type %s not allowed", mime)
	}
	img, err := imaging.Decode(bytes.NewReader(raw), imaging.AutoOrientation(true))
	if err != nil {
		return nil, errors.New("undecodable image")
	}
	sum := fmt.Sprintf("%x", sha256.Sum256(raw))
	ext := ".bin"
	switch mime {
	case "image/jpeg":
		ext = ".jpg"
	case "image/png":
		ext = ".png"
	case "image/webp":
		ext = ".webp"
	case "image/gif":
		ext = ".gif"
	}
	sub := filepath.Join(dir(dataDir), sum[:2])
	if err := os.MkdirAll(sub, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(OriginalPath(dataDir, sum, ext), raw, 0o644); err != nil {
		return nil, err
	}
	if err := writeJPEG(ThumbPath(dataDir, sum), imaging.Fit(img, ThumbLimit, ThumbLimit, imaging.Lanczos)); err != nil {
		return nil, err
	}
	if err := writeJPEG(ModelPath(dataDir, sum), imaging.Fit(img, ModelLimit, ModelLimit, imaging.Lanczos)); err != nil {
		return nil, err
	}
	return &Saved{Filename: fh.Filename, ContentType: mime, ByteSize: int64(len(raw)), SHA256: sum, Ext: ext}, nil
}

func writeJPEG(path string, img image.Image) error {
	var buf bytes.Buffer
	if err := imaging.Encode(&buf, img, imaging.JPEG, imaging.JPEGQuality(85)); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// ImportBytes stores legacy bytes: the original is always kept, derivatives
// are built when the bytes decode (HEIC and friends keep the original only).
func ImportBytes(dataDir, filename, contentType string, raw []byte) (saved *Saved, derived bool, err error) {
	sum := fmt.Sprintf("%x", sha256.Sum256(raw))
	ext := ExtFor(contentType)
	sub := filepath.Join(dir(dataDir), sum[:2])
	if err := os.MkdirAll(sub, 0o755); err != nil {
		return nil, false, err
	}
	if err := os.WriteFile(OriginalPath(dataDir, sum, ext), raw, 0o644); err != nil {
		return nil, false, err
	}
	saved = &Saved{Filename: filename, ContentType: contentType, ByteSize: int64(len(raw)), SHA256: sum, Ext: ext}
	img, derr := imaging.Decode(bytes.NewReader(raw), imaging.AutoOrientation(true))
	if derr != nil {
		return saved, false, nil
	}
	if err := writeJPEG(ThumbPath(dataDir, sum), imaging.Fit(img, ThumbLimit, ThumbLimit, imaging.Lanczos)); err != nil {
		return saved, false, nil
	}
	if err := writeJPEG(ModelPath(dataDir, sum), imaging.Fit(img, ModelLimit, ModelLimit, imaging.Lanczos)); err != nil {
		return saved, false, nil
	}
	return saved, true, nil
}

// Purge removes every file for sha (any original extension).
func Purge(dataDir, sha string) {
	if len(sha) < 2 {
		return
	}
	matches, _ := filepath.Glob(filepath.Join(dir(dataDir), sha[:2], sha+".*"))
	for _, m := range matches {
		_ = os.Remove(m)
	}
}

// ExtFor maps a stored content type back to its file extension.
func ExtFor(mime string) string {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	default:
		return ".bin"
	}
}

// Decodable reports whether raw bytes decode as an image.
func Decodable(raw []byte) bool {
	_, err := imaging.Decode(bytes.NewReader(raw))
	return err == nil
}

// ValidateContentType reports whether an upload content type is accepted.
func ValidateContentType(mime string) bool {
	return allowedTypes[strings.ToLower(strings.TrimSpace(mime))]
}
