// The three shapes an approve may not take: a hold named that is not standing,
// a hold standing that the approve leaves out, and the bare approve — the case
// with nothing named — which is refused wherever a hold stands. Tested at the
// candidate deploy row, so the checks exercise [gate.Holds] alone and not the
// production deploy row's own reads of a real service and a real environment.
package gate_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/gate"
)

func TestApproveRefusesAHoldNotNamedOrLeftOut(t *testing.T) {
	holds := &fakeHolds{standing: []string{gate.HoldDependencyNotLive}}
	s, p := &fakeScore{assessment: assessed(0.2)}, &fakePolicy{applied: applied(0.5)}
	ctx, _, _, g := newGateWith(t, s, p, func(c *gate.Composition) { c.Holds = holds })

	opened, err := g.Fire(ctx, candidateFiring())
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
