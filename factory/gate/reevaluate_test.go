// [gate.Gate.Reevaluate] re-tests the holds standing on a pending row rather
// than firing the gate again: the row stays open while a hold stands, and
// closes on its own once every hold has lifted.
package gate_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/record"
)

func TestReevaluateWaitsWhileAHoldStandsThenClosesOnceItLifts(t *testing.T) {
	holds := &fakeHolds{standing: []string{gate.HoldDependencyNotLive}}
	s, p := &fakeScore{assessment: assessed(0.2)}, &fakePolicy{applied: applied(0.5)}
	ctx, _, _, g := newGateWith(t, s, p, func(c *gate.Composition) { c.Holds = holds })

	opened, err := g.Fire(ctx, candidateFiring())
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if opened.HumanDecides {
		t.Fatal("the number is under the threshold and nothing else marks the row, so no human is at it")
	}

	still, err := g.Reevaluate(ctx, opened)
	if err != nil {
		t.Fatalf("Reevaluate while the hold stands: %v", err)
	}
	if len(still.Holds) != 1 || still.Holds[0] != gate.HoldDependencyNotLive {
		t.Errorf("Reevaluate reports holds %v, want the one still standing", still.Holds)
	}
	if still.Closed.ID != "" {
		t.Errorf("Reevaluate closed %q while a hold stands", still.Closed.ID)
	}

	holds.standing = nil
	closed, err := g.Reevaluate(ctx, opened)
	if err != nil {
		t.Fatalf("Reevaluate once the hold lifted: %v", err)
	}
	if len(closed.Holds) != 0 {
		t.Errorf("Reevaluate reports holds %v, want none once it lifted", closed.Holds)
	}
	if closed.Closed.ID == "" {
		t.Fatal("Reevaluate did not close the row once every hold lifted")
	}
	if closed.Closed.Verdict != string(gate.VerdictApprove) {
		t.Errorf("the row closed as %q, want an auto-approve: the number is under the threshold", closed.Closed.Verdict)
	}
	if closed.Closed.Actor.Kind != record.KindComponent {
		t.Errorf("the auto-approve's actor is %s, want the gate component", closed.Closed.Actor.Kind)
	}
}
