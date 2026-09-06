// The sixth page condition: an open incident whose crossing has not stopped,
// with no window of that service open.
package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/window"
)

// TestAnOpenIncidentWithNoOpenWindowPages is the condition the watch pass reads.
// Once the window has closed, nothing the factory has will roll the release
// back, so a crossing that goes on is the deployed software being worse until a
// human ends it — and the factory having raised an intent from the incident does
// not answer it. Neither half of the condition is an event, so it is read on the
// pass that reads the windows rather than fired by something.
func TestAnOpenIncidentWithNoOpenWindowPages(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)

	if _, err := run(ctx, d, of(theStatement)); err != nil {
		t.Fatalf("the first run stopped: %v\noutput so far:\n%s", err, out)
	}
	d.in = strings.NewReader(approvals)
	res, err := run(ctx, d, of(theSecondStatement))
	if err != nil {
		t.Fatalf("the second run stopped: %v\noutput so far:\n%s", err, out)
	}
	c := only(t, res)
	if open, err := window.CountOpen(ctx, d.pool, res.serviceID); err != nil || open != 0 {
		t.Fatalf("CountOpen = %d, %v; this condition is about a service holding none open", open, err)
	}

	// The software starts failing after its window has closed, which is what
	// raises the incident the page is about. The path is composed before the
	// emission is written, so the units are not aged past the recent history the
	// reading is taken over.
	path := p(ctx, t, d)
	signal := localtarget.SignalFile(d.dir, c.reverifiedBuildID)
	var failing strings.Builder
	for n := range 400 {
		at := time.Now().Add(-2 * time.Second).Add(time.Duration(n) * 5 * time.Millisecond)
		failing.WriteString(at.UTC().Format(time.RFC3339Nano) + "\terror\n")
	}
	if err := os.WriteFile(signal, []byte(failing.String()), 0o644); err != nil {
		t.Fatalf("writing what the running build emits: %v", err)
	}

	if err := path.watchPass(ctx, theServiceRecord(t, ctx, path)); err != nil {
		t.Fatalf("the pass stopped: %v\noutput so far:\n%s", err, out)
	}

	incidents, err := incident.ForService(ctx, d.pool, res.serviceID)
	if err != nil || len(incidents) != 1 {
		t.Fatalf("ForService = %+v, %v, want the one incident the crossing raised", incidents, err)
	}
	raised := incidents[0]
	if !strings.Contains(out.String(), "the page went out") {
		t.Errorf("the pass does not report the page an open incident with no open window fires:\n%s", out)
	}
	events, err := path.notifier.EventsFor(ctx, raised.ID)
	if err != nil {
		t.Fatalf("reading the page events on the incident: %v", err)
	}
	if len(events) != 1 || notifier.Event(events[0].Event) != notifier.EventReached {
		t.Fatalf("the page events on %s are %+v, want one reached", raised.ID, events)
	}

	// A page is the sequence of events on one row: unacknowledged it widens
	// exactly once, to the owner, and there is no second widening however many
	// passes read the same incident.
	for range 2 {
		if err := path.watchPass(ctx, theServiceRecord(t, ctx, path)); err != nil {
			t.Fatalf("a later pass stopped: %v", err)
		}
	}
	again, err := path.notifier.EventsFor(ctx, raised.ID)
	if err != nil {
		t.Fatalf("reading the page events again: %v", err)
	}
	if len(again) != 2 || notifier.Event(again[1].Event) != notifier.EventWidened {
		t.Errorf("the page events on %s are %+v, want the reached one and one widening", raised.ID, again)
	}
}
