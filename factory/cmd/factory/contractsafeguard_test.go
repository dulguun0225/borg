// Tests of a safeguard's predicate placed on a contract element: it
// stops a removal until it is withdrawn.
package main

import (
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/contract"
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

	blocked := only(t, runOne(t, ctx, d, out, removeStatement, theService))
	if blocked.merged {
		t.Fatalf("the removal merged with a safeguard's predicate naming the element:\n%s", out)
	}
	if blocked.autoRejectedBy != gate.AutoRejectedBySafeguardPredicate {
		t.Fatalf("the removal was rejected by %q, want the safeguard's predicate", blocked.autoRejectedBy)
	}
	if !strings.Contains(blocked.checked.Why(), placed.ID) || !strings.Contains(blocked.checked.Why(), actor.Key) {
		t.Errorf("the rejection names neither the safeguard nor its author: %s", blocked.checked.Why())
	}
	// The implementation stage stands at one attempt: an attempt is counted on
	// entry to author, and Dispatch.ReturnTo — what the mechanical rejection
	// sends the item back with — counts nothing itself, nothing here re-entering
	// the stage to author it again.
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
	if attempts != 1 {
		t.Errorf("the implementation stage stands at %d attempts, want 1", attempts)
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
	through := only(t, runOne(t, ctx, d, out, removeStatement, theService))
	if !through.merged {
		t.Fatalf("the removal is still refused after the safeguard was withdrawn:\n%s", out)
	}
}
