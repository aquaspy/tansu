package chat

import "sync"

// Event is one SSE payload: the event name plus a rendered HTML fragment.
type Event struct {
	Name string
	HTML string
}

// Hub routes completion events to the browsers watching a conversation.
// It replaces the Action Cable broadcasts (status/body/message/title/cost).
type Hub struct {
	mu   sync.Mutex
	subs map[int64]map[chan Event]struct{}
}

func NewHub() *Hub { return &Hub{subs: map[int64]map[chan Event]struct{}{}} }

// Subscribe registers a watcher for conversationID. Call the returned
// function to unregister.
func (h *Hub) Subscribe(conversationID int64) (<-chan Event, func()) {
	ch := make(chan Event, 32)
	h.mu.Lock()
	if h.subs[conversationID] == nil {
		h.subs[conversationID] = map[chan Event]struct{}{}
	}
	h.subs[conversationID][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if set, ok := h.subs[conversationID]; ok {
			delete(set, ch)
			if len(set) == 0 {
				delete(h.subs, conversationID)
			}
		}
	}
}

// Publish delivers e to every watcher of the conversation. The channel
// buffer absorbs render bursts; a disconnected watcher stops the send
// when its handler returns.
func (h *Hub) Publish(conversationID int64, e Event) {
	h.mu.Lock()
	var chans []chan Event
	for ch := range h.subs[conversationID] {
		chans = append(chans, ch)
	}
	h.mu.Unlock()
	for _, ch := range chans {
		ch <- e
	}
}
