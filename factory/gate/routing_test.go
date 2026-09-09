// Where a safeguarded row waits: the human a safeguard's own routing field
// names, rather than the owner by default.
package gate_test

import (
	"context"
	"testing"

	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/policy"
)

// TestASafeguardedRowRoutesToTheHumanTheSafeguardNames: a safeguard adds the
// human and names who its rows route to, so the check reaches the person who
// authored it — a compliance officer who safeguards a regulated area answers
// their own rows.
func TestASafeguardedRowRoutesToTheHumanTheSafeguardNames(t *testing.T) {
	const officer = "person:compliance"
	s := &fakeScore{assessment: assessed(0.1)}
	p := &fakePolicy{applied: policy.Applied{
		PolicyVersion: testPolicyVersion, Threshold: 0.9, ThresholdFrom: policy.FromSupplied,
		HumanBySafeguard: true, Safeguards: []string{"sg_0000000000000000000000000000000a"},
	}}
	asked := []string(nil)
	ctx, pool, token, g := newGateWith(t, s, p, func(c *gate.Composition) {
		c.SafeguardRouting = func(_ context.Context, ids []string) (gate.RoutedTo, error) {
			asked = ids
			return gate.RoutedTo{Human: officer}, nil
		}
	})

	// The candidate deploy row names no duty, so it is the row that widens to
	// the owner where nothing routes it.
	opened, err := g.Fire(ctx, candidateFiring(t, ctx, pool, token))
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if len(asked) != 1 || asked[0] != "sg_0000000000000000000000000000000a" {
		t.Fatalf("the routing was read for %v, want the safeguards that applied", asked)
	}
	if opened.WaitsOn.Human != officer {
		t.Fatalf("the row waits on %+v, want the human the safeguard names", opened.WaitsOn)
	}
	if opened.WaitsOn.TheOwner() {
		t.Error("a safeguarded row that names a human still reads as the owner's")
	}

	// A gate composed with no reader of the routing widens to the owner, which
	// is the default the routing field exists to replace.
	ctx, pool, token, bare := newGate(t, s, p)
	opened, err = bare.Fire(ctx, candidateFiring(t, ctx, pool, token))
	if err != nil {
		t.Fatalf("Fire with no routing composed: %v", err)
	}
	if !opened.WaitsOn.TheOwner() {
		t.Errorf("with no routing composed the row waits on %+v, want the owner", opened.WaitsOn)
	}
}

// TestTheRollbackHoldTakesTheSafeguardsHumanWhereNobodyHoldsItsDuty: the row a
// rollback whose revert has not shipped holds waits on duty 10, on the named
// human a safeguard's routing field gives where one names it, and on the owner
// where nobody holds it.
func TestTheRollbackHoldTakesTheSafeguardsHumanWhereNobodyHoldsItsDuty(t *testing.T) {
	const named = "person:undoes-shipped-changes"
	s := &fakeScore{assessment: assessed(0.1)}
	p := &fakePolicy{applied: policy.Applied{
		PolicyVersion: testPolicyVersion, Threshold: 0.9, ThresholdFrom: policy.FromSupplied,
		HumanBySafeguard: true, Safeguards: []string{"sg_0000000000000000000000000000000b"},
	}}
	holds := &fakeHolds{standing: []string{gate.HoldRollbackAwaitingRevert}}
	ctx, pool, token, g := newGateWith(t, s, p, func(c *gate.Composition) {
		c.Holds = holds
		c.SafeguardRouting = func(context.Context, []string) (gate.RoutedTo, error) {
			return gate.RoutedTo{Human: named}, nil
		}
	})

	first := deployFiring(t, ctx, pool, token)
	opened, err := g.Fire(ctx, first)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if opened.WaitsOn.Duty != gate.DutyUndoAShippedChange {
		t.Fatalf("the held row's duty is %d, want the duty that undoes a shipped change", opened.WaitsOn.Duty)
	}
	if opened.WaitsOn.Human != named {
		t.Errorf("nobody holds duty 10, so the row waits on %+v, want the human the safeguard names",
			opened.WaitsOn)
	}

	// A holder of the duty takes it back: the safeguard's human is the fallback
	// between the duty and the owner and not a replacement for the duty.
	declares(t, ctx, pool, token, owner, "person:responder", gate.DutyUndoAShippedChange)
	second := first
	second.ItemID = "it_0000000000000000000000000000000b"
	approvedAbove(t, ctx, pool, token, gate.MergeToMaster, second.ItemID)
	opened, err = g.Fire(ctx, second)
	if err != nil {
		t.Fatalf("Fire with a holder of duty 10: %v", err)
	}
	if opened.WaitsOn.Human != "" || len(opened.WaitsOn.Holders) != 1 {
		t.Errorf("the row waits on %+v, want the holder of duty 10 and no named human", opened.WaitsOn)
	}
}
