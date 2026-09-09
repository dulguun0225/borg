// The seventh page condition: an incident-raised item still being worked past
// its service's incident-item bound.
package main

import (
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/service"
)

// TestIncidentBoundExceededPagesOnceUncleared is the pass wired in passes.go:
// an item decomposed from the intent an incident named, still at spec once the
// service's incident-item bound has passed, pages once and widens once, and a
// pass after that writes nothing further — the shape
// [path.pagesHeldToTheHours] and [path.driftDetectorPages] already page a
// standing condition in.
func TestIncidentBoundExceededPagesOnceUncleared(t *testing.T) {
	ctx, d, out := newPath(t, "")
	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path: %v\n%s", err, out)
	}

	svc, err := service.NewWriter(p.d.pool, p.d.token).Create(ctx, decompositionActor,
		"checkout-bound", "/srv/repos/checkout-bound", record.NewID("proj"))
	if err != nil {
		t.Fatalf("creating the service: %v", err)
	}
	tx, err := p.d.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning the authoring of the bound: %v", err)
	}
	if err := service.SetIncidentItemBound(ctx, tx, svc.ID, 1); err != nil {
		t.Fatalf("SetIncidentItemBound: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("committing the bound: %v", err)
	}

	intentID := record.NewID("int")
	it, err := p.decomposition.Create(ctx, decompositionActor, item.New{
		IntentID: intentID, ServiceID: svc.ID, Branch: "item/bound-test",
		RequirementsAnswered: oneRequirement,
	}, "", "", nil)
	if err != nil {
		t.Fatalf("decomposing the item: %v", err)
	}

	if _, err := incident.NewWriter(p.d.pool, p.d.token).Raise(ctx, healthmonitor.Actor, incident.Raising{
		EnvironmentID: record.NewID("env"), ServiceID: svc.ID, ReleaseID: record.NewID("rel"), DeployID: record.NewID("dep"),
		Reading: incident.ReadingComparison, Quantity: "error_rate", Size: 0.02, Confidence: 0.99,
		BoundaryVersion: "interval-paired-difference/v1", PolicyVersion: "pv_test", ScoreVersion: "scv_test",
		FailureRecords: `[]`, IntentID: intentID,
	}); err != nil {
		t.Fatalf("Raise: %v", err)
	}

	// The pass with nothing yet past the bound writes nothing.
	if moved, err := p.pageOverdueIncidentItems(ctx); err != nil {
		t.Fatalf("pageOverdueIncidentItems before the bound: %v", err)
	} else if moved {
		t.Errorf("pageOverdueIncidentItems before the bound reports a page")
	}

	time.Sleep(1200 * time.Millisecond)

	moved, err := p.pageOverdueIncidentItems(ctx)
	if err != nil {
		t.Fatalf("pageOverdueIncidentItems after the bound: %v", err)
	}
	if !moved {
		t.Fatal("pageOverdueIncidentItems after the bound reports no page")
	}
	events, err := p.notifier.EventsFor(ctx, it.ID)
	if err != nil {
		t.Fatalf("reading the page events on the item: %v", err)
	}
	if len(events) != 1 || notifier.Event(events[0].Event) != notifier.EventReached {
		t.Fatalf("the page events on %s are %+v, want one reached", it.ID, events)
	}

	// The page is the sequence of events on one row: unacknowledged it widens
	// exactly once, to the owner, and a further pass writes nothing further.
	if _, err := p.pageOverdueIncidentItems(ctx); err != nil {
		t.Fatalf("a second pass stopped: %v", err)
	}
	again, err := p.notifier.EventsFor(ctx, it.ID)
	if err != nil {
		t.Fatalf("reading the page events again: %v", err)
	}
	if len(again) != 2 || notifier.Event(again[1].Event) != notifier.EventWidened {
		t.Errorf("the page events on %s are %+v, want the reached one and one widening", it.ID, again)
	}

	moved, err = p.pageOverdueIncidentItems(ctx)
	if err != nil {
		t.Fatalf("a third pass stopped: %v", err)
	}
	if moved {
		t.Error("a third pass reports a page where the page already stands and nothing was acknowledged")
	}
}
