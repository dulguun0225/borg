// Tests of a safeguard's predicate placed on a contract element: it
// stops a removal until it is withdrawn.
package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/safeguard"
	"github.com/dulguun0225/borg/factory/service"
)

// TestASafeguardsPredicateStopsTheRemovalUntilItIsWithdrawn: a safeguard never
// stops the item existing, only passing, and what a reader of that rejection needs
// is the safeguard and its author.
func TestASafeguardsPredicateStopsTheRemovalUntilItIsWithdrawn(t *testing.T) {
	ctx, d, out := newContractPath(t)
	migrated(t, ctx, d, out)

	producer, found, err := service.ByName(ctx, d.pool, theService)
	if err != nil || !found {
		t.Fatalf("reading the producer: found %v, %v", found, err)
	}
	con, found, err := contract.ByName(ctx, d.pool, producer.ID, theHealthInterface)
	if err != nil || !found {
		t.Fatalf("reading the contract: found %v, %v", found, err)
	}
	actor := owner(t, ctx, d.pool, d.token, d.human)
	placed, _, err := policy.NewFactory(d.pool, d.token).AddSafeguard(ctx, actor, gatepolicy.SafeguardPredicate,
		safeguard.Subject{Kind: safeguard.SubjectContractElement, ID: contract.ElementSubject(con.ID, "Health.Detail")},
		safeguard.Bound{Predicate: safeguard.Predicate{Kind: gatepolicy.PredicateRead}}, safeguard.Routing{})
	if err != nil {
		t.Fatalf("adding the safeguard: %v", err)
	}

	// A safeguard's predicate is not something a rebuild fixes on its own:
	// [path.mergeUntilQueued] sends the item back and it comes back naming the
	// same element every time, so the row keeps rejecting it until the stage's
	// own attempt limit is spent and the implementer's dispatch escalates — see
	// [TestAStoresForwardPromiseRefusesAnAlwaysPopulatedColumn] for why this uses
	// [retriedWithNoFix].
	d.in = strings.NewReader(manyApprovals)
	d.model = &retriedWithNoFix{inner: d.model}
	res, err := run(ctx, d, []asked{across(removeStatement, theService)})
	if err == nil {
		t.Fatalf("the removal merged with a safeguard's predicate naming the element:\n%s", out)
	}
	if !errors.Is(err, dispatch.ErrOutOfAttempts) {
		t.Errorf("the error is %v, want a stage out of attempts — every rebuild reproduces the same removal", err)
	}
	blocked := only(t, res)
	if blocked.merged {
		t.Fatalf("the removal merged with a safeguard's predicate naming the element:\n%s", out)
	}
	if blocked.checked == nil || blocked.checked.Check() != gate.AutoRejectedBySafeguardPredicate {
		t.Fatalf("the removal's last completed run was rejected by %q, want the safeguard's predicate", checkOf(blocked))
	}
	if !strings.Contains(blocked.checked.Why(), placed.ID) || !strings.Contains(blocked.checked.Why(), actor.Key) {
		t.Errorf("the rejection names neither the safeguard nor its author: %s", blocked.checked.Why())
	}
	// The implementation stage spent whatever attempt limit is in force —
	// authored or learned, and this test's history may have moved it from what
	// [attemptLimit] reads — rebuilding against a removal no rebuild here fixes,
	// and the item is escalated at it.
	stages, err := item.Stages(ctx, d.pool, blocked.itemID)
	if err != nil {
		t.Fatalf("reading the item's stages: %v", err)
	}
	attempts := 0
	for _, s := range stages {
		if s.Stage == item.StageImplementation {
			attempts = s.Attempts
		}
	}
	if attempts < 2 {
		t.Errorf("the implementation stage stands at %d attempts, want at least 2 — a rebuild happened and then the limit was spent", attempts)
	}
	it, err := item.Get(ctx, d.pool, blocked.itemID)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}
	if it.Stage != item.StageEscalated {
		t.Errorf("the item that spent its attempts is at %s, want escalated", it.Stage)
	}

	// A safeguard leaves force at the gate row A safeguard's withdrawal, which is
	// two writes: the withdrawal record, and the approval that row's close makes.
	// The row is routed away from whoever wrote the withdrawal, so the approval is
	// another human's.
	written, _, err := policy.NewFactory(d.pool, d.token).WriteSafeguardWithdrawal(ctx, actor, placed.ID)
	if err != nil {
		t.Fatalf("writing the withdrawal: %v", err)
	}
	throughASubcommand(t, ctx, &d, func() error {
		return approveCommand([]string{"-safeguard-withdrawal", written.ID, "-human", "reviewer"})
	})
	// The escalated candidate is left as it is — clearing an escalation is not
	// this milestone's concern — and a fresh item asking for the same removal is
	// what confirms the predicate itself, and not the earlier candidate's own
	// standing, is what was blocking it.
	d.model = &contractModel{}
	through := only(t, runOne(t, ctx, d, out, removeStatement, theService))
	if !through.merged {
		t.Fatalf("the removal is still refused after the safeguard was withdrawn:\n%s", out)
	}
}
