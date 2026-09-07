// stream_test.go is white-box, in package screens itself, so that a closed
// client's removal from the subscription map can be read directly rather
// than inferred from the HTTP response alone.
package screens

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestChangedDeliversToASubscriber: a subscriber on an address gets one
// event after Changed names it.
func TestChangedDeliversToASubscriber(t *testing.T) {
	s := &Server{streams: newStreams()}
	ch := s.streams.subscribe("item", "it_1")
	s.Changed("item", "it_1")
	select {
	case <-ch:
	default:
		t.Fatal("Changed did not notify a subscriber on its own address")
	}
}

// TestChangedOnAnItemAlsoReachesWorkAndHome: an item's change is also a
// change to the board it appears on and to the home view's own count.
func TestChangedOnAnItemAlsoReachesWorkAndHome(t *testing.T) {
	s := &Server{streams: newStreams()}
	work := s.streams.subscribe("work", listKindID)
	home := s.streams.subscribe("home", listKindID)
	s.Changed("item", "it_1")
	if len(work) != 1 {
		t.Error("Changed on an item did not reach work")
	}
	if len(home) != 1 {
		t.Error("Changed on an item did not reach home")
	}
}

// TestChangedOnAServiceReachesOps: a service's change is also a change to
// Ops.
func TestChangedOnAServiceReachesOps(t *testing.T) {
	s := &Server{streams: newStreams()}
	ops := s.streams.subscribe("ops", listKindID)
	s.Changed("service", "svc_1:env_1")
	if len(ops) != 1 {
		t.Error("Changed on a service did not reach ops")
	}
}

// TestHandleStreamDeliversAnEventAndRemovesAClosedClient: the HTTP handler
// writes one SSE event per change, and once its client goes away its
// channel is gone from the map too, so a later Changed finds nobody there
// to notify.
func TestHandleStreamDeliversAnEventAndRemovesAClosedClient(t *testing.T) {
	s := &Server{streams: newStreams()}
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest(http.MethodGet, "/api/stream/item/it_1", nil).WithContext(ctx)
	r.SetPathValue("kind", "item")
	r.SetPathValue("id", "it_1")
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		s.handleStream(rec, r)
		close(done)
	}()

	key := addressKey("item", "it_1")
	deadline := time.Now().Add(time.Second)
	for {
		s.streams.mu.Lock()
		n := len(s.streams.subs[key])
		s.streams.mu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("handleStream never subscribed")
		}
		time.Sleep(time.Millisecond)
	}

	s.Changed("item", "it_1")
	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done

	if !strings.Contains(rec.Body.String(), "event: changed") {
		t.Errorf("body = %q, want an event: changed line", rec.Body.String())
	}

	s.streams.mu.Lock()
	defer s.streams.mu.Unlock()
	if len(s.streams.subs[key]) != 0 {
		t.Error("a closed client is still in the map")
	}
}
