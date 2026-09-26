package handler

import (
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/aquasp/kurachat/internal/docs"
	"github.com/aquasp/kurachat/internal/images"
)

// saveAttachment routes one upload to the image or document store.
// PDFs route by sniffed content; docx/pptx route by extension into local
// LibreOffice conversion (OOXML sniffs as plain zip). Exactly one of the
// results is non-nil. convertBin overrides the soffice lookup ("" = PATH).
func saveAttachment(ctx context.Context, dataDir string, fh *multipart.FileHeader, convertBin string) (*images.Saved, *docs.Saved, error) {
	f, err := fh.Open()
	if err != nil {
		return nil, nil, err
	}
	head, err := io.ReadAll(io.LimitReader(f, 512))
	f.Close()
	if err != nil {
		return nil, nil, err
	}
	if docs.ValidateContentType(http.DetectContentType(head)) {
		doc, err := docs.Save(dataDir, fh)
		return nil, doc, err
	}
	if docs.Convertible(fh.Filename) {
		doc, err := saveConverted(ctx, dataDir, fh, convertBin)
		return nil, doc, err
	}
	img, err := images.Save(dataDir, fh)
	return img, nil, err
}

// saveConverted renders an office upload to PDF, then stores it through
// the normal document path (same caps, same download, same file part).
func saveConverted(ctx context.Context, dataDir string, fh *multipart.FileHeader, convertBin string) (*docs.Saved, error) {
	if fh.Size > docs.MaxBytes {
		return nil, errors.New("too large")
	}
	f, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, docs.MaxBytes+1))
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, docs.ConvertTimeout)
	defer cancel()
	pdf, err := docs.ConvertToPDF(ctx, convertBin, fh.Filename, raw)
	if err != nil {
		return nil, err
	}
	return docs.SaveBytes(dataDir, docs.ConvertedName(fh.Filename), pdf)
}
