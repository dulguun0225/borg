package screens

import (
	"fmt"
	"net/http"
	"sync"
)

// listKindID is the id every list kind — home, work, ops, factory, people —
// is addressed under: there is one of each, so an address needs no id of its
// own, and a client sends "-" in its place.
const listKindID = "-"

// streams is the fan-out for every open subscription: one channel per
// subscriber, keyed by the address it is on. A subscriber whose channel is
// full is dropped rather than blocked — a client that missed a change
// re-reads the address whole rather than resuming from what it held, so a
// queued backlog would buy it nothing.
type streams struct {
	mu   sync.Mutex
	subs map[string]map[chan struct{}]struct{}
}

func newStreams() *streams {
	return &streams{subs: map[string]map[chan struct{}]struct{}{}}
}

func addressKey(kind, id string) string { return kind + ":" + id }

func (s *streams) subscribe(kind, id string) chan struct{} {
	ch := make(chan struct{}, 1)
	key := addressKey(kind, id)
	s.mu.Lock()
	if s.subs[key] == nil {
		s.subs[key] = map[chan struct{}]struct{}{}
	}
	s.subs[key][ch] = struct{}{}
	s.mu.Unlock()
	return ch
}

func (s *streams) unsubscribe(kind, id string, ch chan struct{}) {
	key := addressKey(kind, id)
	s.mu.Lock()
	delete(s.subs[key], ch)
	if len(s.subs[key]) == 0 {
		delete(s.subs, key)
	}
	s.mu.Unlock()
}

func (s *streams) notify(kind, id string) {
	key := addressKey(kind, id)
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs[key] {
		select {
		case ch <- struct{}{}:
		default:
			// The subscriber is behind and will re-read the address whole
			// on its next event or on reconnect, so nothing is queued for
			// it here.
		}
	}
}

// Changed fans an address's change out to every subscriber on it, and, per
// the rule the four screens share, to the address that shows a summary of
// it: an item's change also reaches work and home, and a service's change
// also reaches ops. Anything at Factory or People already names its own
// list address, so no second address is notified for either.
func (s *Server) Changed(kind, id string) {
	s.streams.notify(kind, id)
	switch kind {
	case "item":
		s.streams.notify("work", listKindID)
		s.streams.notify("home", listKindID)
	case "service":
		s.streams.notify("ops", listKindID)
	}
}

// handleStream is GET /api/stream/{kind}/{id}: one server-sent-events
// connection per address, writing one "changed" event per call to Changed
// on that address until the client goes away.
func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	id := r.PathValue("id")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "screens: streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch := s.streams.subscribe(kind, id)
	defer s.streams.unsubscribe(kind, id, ch)

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			fmt.Fprint(w, "event: changed\ndata: {}\n\n")
			flusher.Flush()
		}
	}
}
