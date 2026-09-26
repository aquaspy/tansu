package handler

import (
	"net/http"
	"os"
	"strconv"

	"github.com/aquasp/kurachat/internal/images"
	"github.com/aquasp/kurachat/internal/store"
	"github.com/go-chi/chi/v5"
)

// handleImage serves an upload to its owner. Unknown ids 404 without
// distinguishing missing from forbidden.
func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	if user == nil {
		s.notFound(w, r)
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	img, err := s.Store.GetImage(id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	msg, err := s.Store.GetMessage(img.MessageID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if _, err := s.Store.FindConversation(user.ID, msg.ConversationID); err != nil {
		s.notFound(w, r)
		return
	}
	s.serveImage(w, r, img)
}

func (s *Server) serveImage(w http.ResponseWriter, r *http.Request, img *store.Image) {
	path := images.OriginalPath(s.DataDir, img.SHA256, images.ExtFor(img.ContentType))
	mime := img.ContentType
	if r.URL.Query().Get("v") == "thumb" {
		if _, err := os.Stat(images.ThumbPath(s.DataDir, img.SHA256)); err == nil {
			path = images.ThumbPath(s.DataDir, img.SHA256)
			mime = "image/jpeg"
		}
		// else: imported originals without derivatives serve as-is.
	}
	f, err := os.Open(path)
	if err != nil {
		s.notFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		s.notFound(w, r)
		return
	}
	// Content-addressed bytes are immutable.
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Disposition", "inline")
	http.ServeContent(w, r, img.Filename, info.ModTime(), f)
}
