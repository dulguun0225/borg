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
)

func TestAbandonRequiresAReasonAndEndsTheDecision(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, _, _, g := newGate(t, s, p)

	opened, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}

	if _, err := g.Abandon(ctx, opened, ""); !errors.Is(err, gate.ErrReasonMissing) {
		t.Errorf("Abandon with no reason = %v, want ErrReasonMissing", err)
	}

	row, err := g.Abandon(ctx, opened, gate.AbandonedBySupersession)
	if err != nil {
		t.Fatalf("Abandon: %v", err)
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
