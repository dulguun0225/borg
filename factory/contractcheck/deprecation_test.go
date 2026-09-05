// Check.Deprecated and Check.RaiseRemovals: the deprecation list emptying
// releases a breaking change, and a safeguard's predicate blocks the same
// removal until it is withdrawn.
package contractcheck_test

import (
	"slices"
	"testing"

	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/contractcheck"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/safeguard"
	"github.com/dulguun0225/borg/factory/window"
)

// TestTheDeprecationListIsAQueryAndTheDetectorRaisesTheRemovalOnce.
func TestTheDeprecationListIsAQueryAndTheDetectorRaisesTheRemovalOnce(t *testing.T) {
	ctx, g := newGraph(t)

	marked := published(element("Status", "string", true, false), element("Detail", "string", false, true))
	ship(t, ctx, g, g.producer, []contract.Form{marked}, nil, window.ExitTimedOut)
	ship(t, ctx, g, g.consumer, nil, []consumercontract.Draft{
		draft(g.producer, theInterface, "Detail", gatepolicy.PredicateRead, ""),
	}, window.ExitTimedOut)

	list, err := g.check.Deprecated(ctx)
	if err != nil {
		t.Fatalf("Deprecated: %v", err)
	}
	if len(list) != 1 || list[0].Element.Name != "Detail" {
		t.Fatalf("the marked elements are %+v, want Detail alone", list)
	}
	if list[0].Empty() {
		t.Fatal("the list on Detail is empty and the consumer still declares it")
	}
	if !slices.Contains(list[0].Consumers(), g.consumer.ID) {
		t.Errorf("the list names %v, want the consumer", list[0].Consumers())
	}
	if raised, err := g.check.RaiseRemovals(ctx); err != nil || len(raised) != 0 {
		t.Fatalf("the detector raised %d removals while a consumer still declares the element, %v", len(raised), err)
	}

	// The consumer's next release stops reading it. Nothing withdrew anything: the
	// next derivation found nothing, and the query stopped seeing it.
	ship(t, ctx, g, g.consumer, nil, []consumercontract.Draft{
		draft(g.producer, theInterface, "Status", gatepolicy.PredicateRead, ""),
	}, window.ExitTimedOut)

	list, err = g.check.Deprecated(ctx)
	if err != nil {
		t.Fatalf("the second Deprecated: %v", err)
	}
	if len(list) != 1 || !list[0].Empty() {
		t.Fatalf("the list on Detail is %+v after the consumer stopped reading it", list)
	}
	raised, err := g.check.RaiseRemovals(ctx)
	if err != nil {
		t.Fatalf("RaiseRemovals: %v", err)
	}
	if len(raised) != 1 || !raised[0].New {
		t.Fatalf("the detector raised %+v, want one new intent", raised)
	}
	want := contractcheck.RemovalStatement(g.producer.Name, theInterface, "Detail")
	if raised[0].Intent.Statement != want {
		t.Errorf("the intent's statement is %q, want %q", raised[0].Intent.Statement, want)
	}

	// A second pass takes nothing in: the intent is still unrefined, and the
	// statement is the handle.
	again, err := g.check.RaiseRemovals(ctx)
	if err != nil {
		t.Fatalf("the second RaiseRemovals: %v", err)
	}
	if len(again) != 1 || again[0].New || again[0].Intent.ID != raised[0].Intent.ID {
		t.Fatalf("the second pass raised %+v, want the intent the first one took in", again)
	}
}

// TestASafeguardsPredicateBlocksTheRemovalAndIsToldApartFromAConsumerContract: a
// safeguard never stops the item existing, only passing, and what a reader of that
// rejection needs is the safeguard and its author.
func TestASafeguardsPredicateBlocksTheRemovalAndIsToldApartFromAConsumerContract(t *testing.T) {
	ctx, g := newGraph(t)

	full := published(element("Status", "string", true, false), element("Detail", "string", false, true))
	ship(t, ctx, g, g.producer, []contract.Form{full}, nil, window.ExitTimedOut)

	con, found, err := contract.ByName(ctx, g.pool, g.producer.ID, theInterface)
	if err != nil || !found {
		t.Fatalf("ByName = found %v, %v", found, err)
	}
	placed, _, err := g.factory.AddSafeguard(ctx, theOwner, gatepolicy.SafeguardPredicate,
		safeguard.Subject{Kind: safeguard.SubjectContractElement, ID: contract.ElementSubject(con.ID, "Detail")},
		safeguard.Bound{Predicate: safeguard.Predicate{Kind: gatepolicy.PredicateRead}})
	if err != nil {
		t.Fatalf("adding the safeguard: %v", err)
	}

	// The detector still raises the removal — a safeguard never stops the item existing.
	raised, err := g.check.RaiseRemovals(ctx)
	if err != nil {
		t.Fatalf("RaiseRemovals: %v", err)
	}
	if len(raised) != 1 || !raised[0].New {
		t.Fatalf("the detector raised %+v with a safeguard standing, want the removal intent", raised)
	}

	trimmed := published(element("Status", "string", true, false))
	removing := candidateOf(t, ctx, g, g.producer, []contract.Form{trimmed}, nil, ok())
	checked, err := g.check.Enforce(ctx, removing, g.production)
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if checked.Passed() {
		t.Fatal("the removal passed with a safeguard's predicate naming the element")
	}
	if checked.Check() != gate.AutoRejectedBySafeguardPredicate {
		t.Errorf("the check that rejected is %q, want the safeguard's predicate", checked.Check())
	}
	if !contains(checked.Why(), placed.ID) || !contains(checked.Why(), theOwner.Key) {
		t.Errorf("the rejection names neither the safeguard nor its author: %s", checked.Why())
	}

	// Withdrawing it lets the next candidate through, which is how an invented read
	// is taken back.
	if _, err := g.factory.WithdrawSafeguard(ctx, theOwner, placed.ID); err != nil {
		t.Fatalf("withdrawing the safeguard: %v", err)
	}
	after := candidateOf(t, ctx, g, g.producer, []contract.Form{trimmed}, nil, ok())
	checked, err = g.check.Enforce(ctx, after, g.production)
	if err != nil {
		t.Fatalf("the second Enforce: %v", err)
	}
	if !checked.Passed() {
		t.Fatalf("the removal is still refused after the safeguard was withdrawn: %s", checked.Why())
	}
}
