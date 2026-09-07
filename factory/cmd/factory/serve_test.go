// The process the lease was always for: every component's pass reachable on its
// own, the one route it serves until the screens replace it, and the
// demonstration the resumable pass exists for — a row a pass leaves waiting in
// Work, closed by a human through the gate component, and the item moving on the
// next pass.
package main

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/clientdist"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/screens"
)

// TestEveryPassRunsWithNothingToDo ticks each of the process's passes once
// against an install that has taken nothing in. Each is a component's own pass
// and a pass with nothing to do is not an error — which is what a process that
// starts before any intent arrives is.
func TestEveryPassRunsWithNothingToDo(t *testing.T) {
	ctx, d, out := newPath(t, "")
	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path: %v\n%s", err, out)
	}
	ps := newPasses(p, nil, nil)
	for _, name := range passOrder {
		if err := ps.Tick(ctx, name); err != nil {
			t.Errorf("the %s pass with nothing to do: %v\noutput so far:\n%s", name, err, out)
		}
	}
	if err := ps.Tick(ctx, "no-such-pass"); err == nil {
		t.Error("a name no pass has was run rather than refused")
	}
}

// TestAPassResumesAnItemAHumanDecided is the resumable pass demonstrated. One
// pass fires the Spec row, finds a human decides it, and leaves it pending in
// Work; nothing in the process closes it. A human closes it through
// [gate.Gate.Decide], which is the call a screen makes, and the next pass reads
// the item back out of the records and continues from that verdict — the
// implementation plan authored by a pass that never held the spec in memory.
func TestAPassResumesAnItemAHumanDecided(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path: %v\n%s", err, out)
	}
	var s shipped
	if err := p.takeIn(ctx, &s, of(theStatement)); err != nil {
		t.Fatalf("taking the intent in: %v\n%s", err, out)
	}
	itemID := only(t, s).itemID

	ps := newPasses(p, nil, nil)
	if err := ps.Tick(ctx, passAdvance); err != nil {
		t.Fatalf("the first advance pass: %v\n%s", err, out)
	}
	pending, err := p.gate.Pending(ctx)
	if err != nil {
		t.Fatalf("reading the pending rows: %v", err)
	}
	if len(pending) != 1 || pending[0].Gate.Kind != gate.KindSpec {
		t.Fatalf("the pass left %d row(s) pending, want the Spec row alone: %v\n%s", len(pending), pending, out)
	}
	if !strings.Contains(out.String(), "Waiting in Work at "+gate.Spec.String()+" on "+itemID) {
		t.Errorf("the pass does not report the item waiting in Work where it asked for a verdict before:\n%s", out)
	}
	if _, found, err := artifact.NewestOfKind(ctx, d.pool, itemID, artifact.KindImplementationPlan); err != nil {
		t.Fatalf("reading the plan version: %v", err)
	} else if found {
		t.Fatal("the pass authored an implementation plan over a spec nobody had decided")
	}

	// A second pass with the row still pending performs nothing: the item is
	// waiting on a human and the process is not stuck, it is shown as stuck.
	before := out.Len()
	if err := ps.Tick(ctx, passAdvance); err != nil {
		t.Fatalf("the second advance pass: %v\n%s", err, out)
	}
	if !strings.Contains(out.String()[before:], "Waiting in Work") {
		t.Errorf("a pass over an item waiting on a human does not say so:\n%s", out.String()[before:])
	}
	if p.moved {
		t.Error("a pass over an item waiting on a human moved something")
	}

	// The human at Work, through the gate component's own call — which is what
	// the screens will do and what this test does directly.
	acted, err := p.d.decide(ctx, p)
	if err != nil {
		t.Fatalf("deciding the Spec row at Work: %v\n%s", err, out)
	}
	if acted != 1 {
		t.Fatalf("the human acted on %d row(s), want the one pending", acted)
	}

	if err := ps.Tick(ctx, passAdvance); err != nil {
		t.Fatalf("the pass after the verdict: %v\n%s", err, out)
	}
	if !p.moved {
		t.Error("the pass after a human's verdict moved nothing")
	}
	if _, found, err := artifact.NewestOfKind(ctx, d.pool, itemID, artifact.KindImplementationPlan); err != nil {
		t.Fatalf("reading the plan version: %v", err)
	} else if !found {
		t.Fatalf("the pass after the verdict did not continue the item to its implementation plan:\n%s", out)
	}
}

// TestHealthzAnswersWithTheFactoryVersion is the one route this process serves
// beside the four screens: it carries neither the factory version nor a
// principal, because it is what a reader outside the process reads before it
// has either.
func TestHealthzAnswersWithTheFactoryVersion(t *testing.T) {
	handler := served(screens.New(nil, nil, factoryVersion, clientdist.Browser()))
	recorded := httptest.NewRecorder()
	handler.ServeHTTP(recorded, httptest.NewRequest("GET", "/healthz", nil))
	if recorded.Code != 200 {
		t.Errorf("GET /healthz answered %d, want 200", recorded.Code)
	}
	if strings.TrimSpace(recorded.Body.String()) != factoryVersion {
		t.Errorf("GET /healthz answered %q, want the factory version %q", recorded.Body.String(), factoryVersion)
	}
	other := httptest.NewRecorder()
	handler.ServeHTTP(other, httptest.NewRequest("POST", "/healthz", nil))
	if other.Code == 200 {
		t.Error("POST /healthz was served, and the route is a read")
	}
}
