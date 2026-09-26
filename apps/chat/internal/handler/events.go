package handler

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aquasp/kurachat/internal/chat"
)

// handleConversationEvents streams completion fragments as SSE. One stream
// per open chat page; the htmx SSE extension swaps each event into its
// subscribed element.
func (s *Server) handleConversationEvents(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	convID, err := convID(r)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if _, err := s.Store.FindConversation(user.ID, convID); err != nil {
		s.notFound(w, r)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ch, unsub := s.Chat.Hub.Subscribe(convID)
	defer unsub()
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			_, _ = fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case e := <-ch:
			writeSSE(w, e)
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, e chat.Event) {
	_, _ = fmt.Fprintf(w, "event: %s\n", e.Name)
	for _, line := range strings.Split(e.HTML, "\n") {
		_, _ = fmt.Fprintf(w, "data: %s\n", strings.TrimRight(line, "\r"))
	}
	_, _ = fmt.Fprint(w, "\n")
}
