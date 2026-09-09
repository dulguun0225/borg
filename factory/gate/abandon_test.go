// [gate.Gate.Abandon]: a reason is required, the abandonment carries no
// verdict, and it ends the decision so a later close on the same open event is
// refused.
//
// [gate.Gate.EnforceAttemptLimit] is not tested here: it reads the item's own
// per-stage count through [item.Stages] and writes the escalation through
// [item.Dispatch], both of which need a real item record this package's fixtures
// do not create. That is an open point of this test suite and not a refusal
// this file demonstrates.
package gate_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/record"
)

// decomposing is the component that writes a supersession onto an item, which
// is the actor an abandonment of that item's rows carries.
var decomposing = record.Actor{Kind: record.KindComponent, Key: "decomposition", Basis: record.BasisClaimed}

func TestAbandonRequiresAReasonAndEndsTheDecision(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, token, g := newGate(t, s, p)

	opened, err := g.Fire(ctx, mergeRowFiring(t, ctx, pool, token))
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}

	if _, err := g.Abandon(ctx, opened, decomposing, ""); !errors.Is(err, gate.ErrReasonMissing) {
		t.Errorf("Abandon with no reason = %v, want ErrReasonMissing", err)
	}

	// The actor is the component that wrote the supersession onto the item and
	// not the gate: the gate is the actor only where the gate itself ended the
	// decision.
	row, err := g.Abandon(ctx, opened, decomposing, gate.AbandonedBySupersession)
	if err != nil {
		t.Fatalf("Abandon: %v", err)
	}
	if row.Actor != decomposing {
		t.Errorf("the abandonment's actor is %+v, want the component that superseded the item", row.Actor)
	}
	if row.Closes != opened.Row.ID {
		t.Errorf("the abandonment closes %q, want %q", row.Closes, opened.Row.ID)
	}
	if row.Verdict != "" {
		t.Errorf("the abandonment carries verdict %q, and an abandonment gives none", row.Verdict)
	}
	if row.Reason != gate.AbandonedBySupersession {
		t.Errorf("the abandonment's reason is %q, want %q", row.Reason, gate.AbandonedBySupersession)
	}

	// A close on an abandoned row is refused by the writer's own rule: the
	// decision already ended without a verdict.
	if _, err := g.Decide(ctx, opened, gate.Given{Actor: owner, Verdict: gate.VerdictApprove}); err == nil {
		t.Error("Decide on an abandoned row was accepted")
	}
}
