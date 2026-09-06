// The halt at a deploy row: the one hold no approve passes, and the two
// exceptions it takes — a revert, and an item the health monitor raised on that
// service, both of them items of an intent the health monitor raised.
package gate_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/halt"
)

func TestAHaltHoldsEveryItemButTheTwoExceptions(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.2)}, &fakePolicy{applied: applied(0.5)}
	raised := false
	ctx, pool, token, g := newGateWith(t, s, p, func(c *gate.Composition) {
		c.RaisedByTheHealthMonitor = func(context.Context, string) (bool, error) { return raised, nil }
	})

	if _, err := halt.NewWriter(pool, token).Insert(ctx, owner, "the owner stopped the factory"); err != nil {
		t.Fatalf("setting the halt: %v", err)
	}

	firing := deployFiring(t, ctx, pool, token)
	held, err := g.Fire(ctx, firing)
	if err != nil {
		t.Fatalf("Fire while a halt stands: %v", err)
	}
	if !slices.Contains(held.Holds, gate.HoldHalt) {
		t.Fatalf("the firing's holds are %v, want the halt among them", held.Holds)
	}
	if _, err := g.Decide(ctx, held, gate.Given{
		Actor: owner, Verdict: gate.VerdictApprove, Holds: []string{gate.HoldHalt},
	}); !errors.Is(err, gate.ErrApproveThroughAHalt) {
		t.Errorf("an approve naming the halt = %v, want ErrApproveThroughAHalt", err)
	}

	// The same halt, over an item of an intent the health monitor raised: a
	// revert and an item raised on the service both read this way, and a halt
	// stops the factory acting forward and never stops it undoing what it did.
	raised = true
	exception := firing
	exception.ItemID = "it_0000000000000000000000000000000c"
	passing, err := g.Fire(ctx, exception)
	if err != nil {
		t.Fatalf("Fire over an item the health monitor raised: %v", err)
	}
	if slices.Contains(passing.Holds, gate.HoldHalt) {
		t.Fatalf("the exception's holds are %v, want the halt not among them", passing.Holds)
	}
	if _, err := g.AutoPass(ctx, passing); err != nil {
		t.Errorf("passing the exception through: %v", err)
	}
}

// TestAGateWithNoReaderOfTheExceptionHoldsEveryItem: a factory composed with no
// reader excepts nothing, so the halt holds every item, which is the safe end
// of the two.
func TestAGateWithNoReaderOfTheExceptionHoldsEveryItem(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.2)}, &fakePolicy{applied: applied(0.5)}
	ctx, pool, token, g := newGate(t, s, p)

	if _, err := halt.NewWriter(pool, token).Insert(ctx, owner, "the owner stopped the factory"); err != nil {
		t.Fatalf("setting the halt: %v", err)
	}

	held, err := g.Fire(ctx, deployFiring(t, ctx, pool, token))
	if err != nil {
		t.Fatalf("Fire while a halt stands: %v", err)
	}
	if !slices.Contains(held.Holds, gate.HoldHalt) {
		t.Errorf("the firing's holds are %v, want the halt among them", held.Holds)
	}
}
