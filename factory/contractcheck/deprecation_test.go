// Check.Deprecated and Check.Raise: the deprecation list emptying raises a
// brownout, that brownout's window establishing volume raises the removal, and
// a safeguard's predicate blocks the same removal until it is withdrawn.
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

// TestTheDeprecationListIsAQueryAndTheDetectorRaisesTheBrownoutThenTheRemoval:
// the list emptying raises the brownout alone, and the removal follows only on
// that brownout's own window having reached its cap uncrossed having received
// volume — nobody has to remember the last two items of a migration.
func TestTheDeprecationListIsAQueryAndTheDetectorRaisesTheBrownoutThenTheRemoval(t *testing.T) {
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
	if raised, err := g.check.Raise(ctx); err != nil || len(raised) != 0 {
		t.Fatalf("the detector raised %d brownouts or removals while a consumer still declares the element, %v", len(raised), err)
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

	// The emptied list raises the brownout, and the brownout alone: the last
	// inference before something is destroyed is checked against the running world
	// instead of trusted.
	raised, err := g.check.Raise(ctx)
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	if len(raised) != 1 || !raised[0].New || !raised[0].Brownout {
		t.Fatalf("the detector raised %+v, want one new brownout", raised)
	}
	wantBrownout := contractcheck.BrownoutStatement(g.producer.Name, theInterface, "Detail")
	if raised[0].Intent.Statement != wantBrownout {
		t.Errorf("the brownout's statement is %q, want %q", raised[0].Intent.Statement, wantBrownout)
	}

	// A second pass takes nothing in: the brownout has not shipped yet, and the
	// evidence is the handle that stops a second one arriving beside it.
	again, err := g.check.Raise(ctx)
	if err != nil {
		t.Fatalf("the second Raise: %v", err)
	}
	if len(again) != 1 || again[0].New || again[0].Intent.ID != raised[0].Intent.ID {
		t.Fatalf("the second pass raised %+v, want the brownout intent the first one took in", again)
	}

	// The brownout ships: a release on the same evidence that disables Detail in
	// behaviour while leaving the form unchanged, its window run to the cap having
	// received volume on both arms. Its own intent finishing is what lets the
	// removal be raised newly on the same evidence, the evidence key otherwise
	// still naming the brownout as the one intent not yet finished on it.
	shipOnIntent(t, ctx, g, g.producer, raised[0].Intent.ID, []contract.Form{marked}, nil, window.ExitTimedOut)
	finishIntent(t, ctx, g, raised[0].Intent.ID)

	removed, err := g.check.Raise(ctx)
	if err != nil {
		t.Fatalf("Raise after the brownout shipped: %v", err)
	}
	if len(removed) != 1 || !removed[0].New || removed[0].Brownout {
		t.Fatalf("the detector raised %+v, want one new removal", removed)
	}
	wantRemoval := contractcheck.RemovalStatement(g.producer.Name, theInterface, "Detail")
	if removed[0].Intent.Statement != wantRemoval {
		t.Errorf("the removal's statement is %q, want %q", removed[0].Intent.Statement, wantRemoval)
	}

	// A further pass takes nothing in: the removal intent is still unrefined, and
	// the statement is the handle.
	again2, err := g.check.Raise(ctx)
	if err != nil {
		t.Fatalf("the third Raise: %v", err)
	}
	if len(again2) != 1 || again2[0].New || again2[0].Intent.ID != removed[0].Intent.ID {
		t.Fatalf("the third pass raised %+v, want the removal intent the previous one took in", again2)
	}
}

// TestABrownoutsReleaseIsToldFromEveryOtherRelease: the health monitor has to be
// told which release is a brownout, because a brownout's window is not an
// ordinary one — it runs to the cap rather than stopping where the boundary
// would allow, and it is the one window that reads more than the producer's own
// numbers. Nothing writes a link from the element to the release, so what says a
// release is one is the walk over the intent's evidence.
func TestABrownoutsReleaseIsToldFromEveryOtherRelease(t *testing.T) {
	ctx, g := newGraph(t)

	marked := published(element("Status", "string", true, false), element("Detail", "string", false, true))
	ordinary, _ := ship(t, ctx, g, g.producer, []contract.Form{marked}, nil, window.ExitTimedOut)

	// Nothing derived names Detail, so the list is empty and the brownout is
	// raised on the mark's evidence.
	raised, err := g.check.Raise(ctx)
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	if len(raised) != 1 || !raised[0].Brownout {
		t.Fatalf("the detector raised %+v, want the brownout", raised)
	}

	brownout, watching := shipOnIntent(t, ctx, g, g.producer, raised[0].Intent.ID, []contract.Form{marked}, nil, "")
	of, is, err := g.check.IsBrownout(ctx, brownout.ID)
	if err != nil {
		t.Fatalf("IsBrownout on the brownout's release: %v", err)
	}
	if !is || of.Element != "Detail" {
		t.Fatalf("IsBrownout on the brownout's release = %+v, %v; want the brownout of Detail", of, is)
	}

	if _, is, err := g.check.IsBrownout(ctx, ordinary.ID); err != nil || is {
		t.Fatalf("IsBrownout on an ordinary release = %v, %v; want false", is, err)
	}

	// The removal is raised on the same evidence once the brownout's window has
	// reached its cap having received volume, and its own release is not a
	// brownout: the oldest release on the evidence is.
	if _, err := g.windows.Close(ctx, watching, window.ExitTimedOut, closedOn()); err != nil {
		t.Fatalf("closing the brownout's window: %v", err)
	}
	finishIntent(t, ctx, g, raised[0].Intent.ID)
	removed, err := g.check.Raise(ctx)
	if err != nil {
		t.Fatalf("Raise after the brownout closed: %v", err)
	}
	if len(removed) != 1 || removed[0].Brownout {
		t.Fatalf("the detector raised %+v, want the removal", removed)
	}
	removal, _ := shipOnIntent(t, ctx, g, g.producer, removed[0].Intent.ID, []contract.Form{marked}, nil, window.ExitTimedOut)
	if _, is, err := g.check.IsBrownout(ctx, removal.ID); err != nil || is {
		t.Fatalf("IsBrownout on the removal's release = %v, %v; want false", is, err)
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
		safeguard.Bound{Predicate: safeguard.Predicate{Kind: gatepolicy.PredicateRead}}, safeguard.Routing{})
	if err != nil {
		t.Fatalf("adding the safeguard: %v", err)
	}

	// The detector still raises the brownout and, once it ships and its window
	// establishes, the removal — a safeguard never stops either item existing.
	raised, err := g.check.Raise(ctx)
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	if len(raised) != 1 || !raised[0].New || !raised[0].Brownout {
		t.Fatalf("the detector raised %+v with a safeguard standing, want the brownout intent", raised)
	}
	shipOnIntent(t, ctx, g, g.producer, raised[0].Intent.ID, []contract.Form{full}, nil, window.ExitTimedOut)
	finishIntent(t, ctx, g, raised[0].Intent.ID)
	removed, err := g.check.Raise(ctx)
	if err != nil {
		t.Fatalf("Raise after the brownout shipped: %v", err)
	}
	if len(removed) != 1 || !removed[0].New || removed[0].Brownout {
		t.Fatalf("the detector raised %+v with a safeguard standing, want the removal intent", removed)
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
	// is taken back — and it takes two writes, the withdrawal and the approval
	// the gate row that decides it makes.
	written, _, err := g.factory.WriteSafeguardWithdrawal(ctx, theOwner, placed.ID)
	if err != nil {
		t.Fatalf("writing the withdrawal: %v", err)
	}
	if _, err := g.factory.ApproveSafeguardWithdrawal(ctx, theApprover, written.ID, decidedAtARow); err != nil {
		t.Fatalf("approving the withdrawal: %v", err)
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
