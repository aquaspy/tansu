package handler

import (
	"net/http"
	"os"
	"strconv"

	"github.com/aquasp/kurachat/internal/docs"
	"github.com/aquasp/kurachat/internal/store"
	"github.com/go-chi/chi/v5"
)

// handleDocument serves an uploaded PDF to its owner. Unknown ids 404
// without distinguishing missing from forbidden.
func (s *Server) handleDocument(w http.ResponseWriter, r *http.Request) {
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
	doc, err := s.Store.GetDocument(id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	msg, err := s.Store.GetMessage(doc.MessageID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if _, err := s.Store.FindConversation(user.ID, msg.ConversationID); err != nil {
		s.notFound(w, r)
		return
	}
	s.serveDocument(w, r, doc)
}

func (s *Server) serveDocument(w http.ResponseWriter, r *http.Request, doc *store.Document) {
	f, err := os.Open(docs.OriginalPath(s.DataDir, doc.SHA256))
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
	w.Header().Set("Content-Type", docs.PDFMime)
	w.Header().Set("Content-Disposition", "inline")
	http.ServeContent(w, r, doc.Filename, info.ModTime(), f)
}
