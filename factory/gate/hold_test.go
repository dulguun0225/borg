// The three shapes an approve may not take: a hold named that is not standing,
// a hold standing that the approve leaves out, and the bare approve — the case
// with nothing named — which is refused wherever a hold stands. Tested at the
// candidate deploy row, so the checks exercise [gate.Holds] alone and not the
// production deploy row's own reads of a real service and a real environment.
package gate_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/dulguun0225/borg/factory/gate"
)

// TestTheDriftMismatchIsOneOfTheHoldsStanding: what the drift detector found
// is the other kind of hold and the only one the factory sets — the deploy is
// held, no evidence the factory can gather lifts it, and an approve names it
// among the set the way it names every other hold.
func TestTheDriftMismatchIsOneOfTheHoldsStanding(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.2)}, &fakePolicy{applied: applied(0.5)}
	found := fakeDrift{found: true, why: "the record says two instances and one is running"}
	ctx, pool, token, g := newGateWith(t, s, p, func(c *gate.Composition) { c.DriftDetector = found })

	opened, err := g.Fire(ctx, deployFiring(t, ctx, pool, token))
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if !slices.Contains(opened.Holds, gate.HoldDriftMismatch) {
		t.Fatalf("the firing's holds are %v, want the mismatch among them", opened.Holds)
	}
	if opened.Mismatch != found.why {
		t.Errorf("the open event says %q disagrees, want what the detector found", opened.Mismatch)
	}
	if !opened.Holding() {
		t.Error("a row a mismatch holds does not read as holding")
	}

	// A bare approve while it stands is refused, and an approve naming it goes
	// through: the human is saying the record is wrong and the deploy should
	// proceed anyway.
	if _, err := g.Decide(ctx, opened, gate.Given{Actor: owner, Verdict: gate.VerdictApprove}); !errors.Is(err, gate.ErrApproveLeavesAHoldOut) {
		t.Errorf("a bare approve under a mismatch = %v, want ErrApproveLeavesAHoldOut", err)
	}
	if _, err := g.Decide(ctx, opened, gate.Given{
		Actor: owner, Verdict: gate.VerdictApprove, Holds: []string{gate.HoldDriftMismatch},
	}); err != nil {
		t.Errorf("an approve naming the mismatch: %v", err)
	}
}

func TestApproveRefusesAHoldNotNamedOrLeftOut(t *testing.T) {
	holds := &fakeHolds{standing: []string{gate.HoldDependencyNotLive}}
	s, p := &fakeScore{assessment: assessed(0.2)}, &fakePolicy{applied: applied(0.5)}
	ctx, pool, token, g := newGateWith(t, s, p, func(c *gate.Composition) { c.Holds = holds })

	opened, err := g.Fire(ctx, candidateFiring(t, ctx, pool, token))
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if len(opened.Holds) != 1 || opened.Holds[0] != gate.HoldDependencyNotLive {
		t.Fatalf("the firing's holds are %v, want the one standing", opened.Holds)
	}

	if _, err := g.AutoPass(ctx, opened); !errors.Is(err, gate.ErrApproveLeavesAHoldOut) {
		t.Errorf("AutoPass while a hold stands = %v, want ErrApproveLeavesAHoldOut", err)
	}
	if _, err := g.Decide(ctx, opened, gate.Given{Actor: owner, Verdict: gate.VerdictApprove}); !errors.Is(err, gate.ErrApproveLeavesAHoldOut) {
		t.Errorf("a bare approve while a hold stands = %v, want ErrApproveLeavesAHoldOut", err)
	}
	if _, err := g.Decide(ctx, opened, gate.Given{
		Actor: owner, Verdict: gate.VerdictApprove, Holds: []string{gate.HoldNoRoomOnThePlatform},
	}); !errors.Is(err, gate.ErrApproveNamesAHoldNotStanding) {
		t.Errorf("naming a hold that is not standing = %v, want ErrApproveNamesAHoldNotStanding", err)
	}

	closing, err := g.Decide(ctx, opened, gate.Given{
		Actor: owner, Verdict: gate.VerdictApprove, Holds: []string{gate.HoldDependencyNotLive},
	})
	if err != nil {
		t.Fatalf("naming the hold standing: %v", err)
	}
	if closing.Verdict != string(gate.VerdictApprove) {
		t.Errorf("the closing's verdict is %q, want approve", closing.Verdict)
	}
}
