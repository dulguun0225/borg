// [gate.Gate.Refer]: a reason is required the way a reject's and a hold's is,
// and a refer where the row already waits on the owner — nobody holding its
// duty, which is every row's starting position with no People declaration
// written — has nobody left to refer to.
package gate_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/gate"
)

func TestReferRequiresAReasonAndRefusesWithNobodyLeft(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, _, _, g := newGate(t, s, p)

	opened, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}

	if _, err := g.Refer(ctx, opened, owner, "", mergeFiring); !errors.Is(err, gate.ErrReasonMissing) {
		t.Errorf("Refer with no reason = %v, want ErrReasonMissing", err)
	}

	// Nobody holds duty 7 (UAT), which the merge row waits on, so the row
	// already waits on the owner and a refer has nobody left to reach.
	if _, err := g.Refer(ctx, opened, owner, "I cannot judge this myself", mergeFiring); !errors.Is(err, gate.ErrNobodyLeftToReferTo) {
		t.Errorf("Refer with nobody left = %v, want ErrNobodyLeftToReferTo", err)
	}

	// Decide refuses a refer outright: it is given through Refer, which closes
	// the row and re-fires it in one call.
	if _, err := g.Decide(ctx, opened, gate.Given{Actor: owner, Verdict: gate.VerdictRefer}); !errors.Is(err, gate.ErrReferGivenHere) {
		t.Errorf("Decide(refer) = %v, want ErrReferGivenHere", err)
	}
}
