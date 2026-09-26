package openrouter

// FileOptions configures PDF parsing via the `file-parser` plugin.
// Engine: mistral-ocr (default, best for scans), cloudflare-ai (free),
// native (models with file input, billed as tokens).
type FileOptions struct {
	Engine string
}

// plugin renders the {"id": "file-parser", ...} payload.
func (o *FileOptions) plugin() map[string]any {
	p := map[string]any{"id": "file-parser"}
	if o == nil {
		return p
	}
	if o.Engine != "" {
		p["pdf"] = map[string]any{"engine": o.Engine}
	}
	return p
}
