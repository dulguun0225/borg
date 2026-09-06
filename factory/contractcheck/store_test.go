// The store rule: the middle items of a migration are decided against the
// candidate environment's own store state rather than the diff, the item that
// moves reads and the drop after it each wait on a deploy record marking the
// backfill complete, and a schema change is exercised by applying it twice and,
// where it destroys stored data, by a snapshot.
package contractcheck_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/contractcheck"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/window"
)

// TestADropOfADeprecatedStoreElementWaitsOnItsBackfill: the drop is the last
// item of a migration, and it is refused while no deploy record marks the
// element's backfill complete — the drop would otherwise destroy the only copy.
func TestADropOfADeprecatedStoreElementWaitsOnItsBackfill(t *testing.T) {
	ctx, g := newGraph(t)

	// The running form already marks ID deprecated: the earlier items of the
	// migration — the addition beside it and every consumer's move — are taken as
	// already shipped, and what this test is over is the drop alone.
	ship(t, ctx, g, g.producer, []contract.Form{stored(element("ID", "string", true, true))}, nil, window.ExitTimedOut)

	dropping := candidateOf(t, ctx, g, g.producer, []contract.Form{stored()}, nil, nil)
	checked, err := g.check.Enforce(ctx, dropping, g.production)
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if checked.Passed() {
		t.Fatal("a drop of a deprecated element with no backfill marked complete passed")
	}
	if checked.Check() != gate.AutoRejectedByContractDiff {
		t.Errorf("the check that rejected is %q, want the store rule", checked.Check())
	}
	if !contains(checked.Why(), "backfill") {
		t.Errorf("the rejection does not name the backfill: %s", checked.Why())
	}
	if len(checked.Migrations) != 1 || len(checked.Migrations[0].Waiting) != 1 ||
		!checked.Migrations[0].Waiting[0].Dropping {
		t.Fatalf("the migration found is %+v, want one waiting element and it dropping", checked.Migrations)
	}

	// A deploy record marking the backfill complete is what clears it.
	markBackfill(t, ctx, g, g.producer, theStore, "ID", "ID_new")
	again := candidateOf(t, ctx, g, g.producer, []contract.Form{stored()}, nil, nil)
	checked, err = g.check.Enforce(ctx, again, g.production)
	if err != nil {
		t.Fatalf("the second Enforce: %v", err)
	}
	if !checked.Passed() {
		t.Fatalf("the drop is still refused once the backfill is marked complete: %s", checked.Why())
	}
}

// TestAMoveOfReadsToAStoresNewFormWaitsOnItsBackfill: the item that moves a
// consumer's reads away from a deprecated element is refused the same way,
// because every row the copy has not reached would read as absent.
func TestAMoveOfReadsToAStoresNewFormWaitsOnItsBackfill(t *testing.T) {
	ctx, g := newGraph(t)

	// The producer's own past release reads the old element, which is what makes
	// the producer its own store's consumer — a store promises forward because
	// its consumer is the release a rollback would restore.
	ship(t, ctx, g, g.producer, []contract.Form{stored(element("Old", "string", true, false))},
		[]consumercontract.Draft{draft(g.producer, theStore, "Old", gatepolicy.PredicateRead, "")}, window.ExitTimedOut)
	// The first item of the migration: New is added beside Old, and Old is
	// marked deprecated, before anything moves its reads to it — the code still
	// reads Old alone.
	ship(t, ctx, g, g.producer,
		[]contract.Form{stored(element("Old", "string", true, true), element("New", "string", false, false))},
		[]consumercontract.Draft{draft(g.producer, theStore, "Old", gatepolicy.PredicateRead, "")}, window.ExitTimedOut)

	moving := candidateOf(t, ctx, g, g.producer,
		[]contract.Form{stored(element("Old", "string", true, true), element("New", "string", false, false))},
		[]consumercontract.Draft{draft(g.producer, theStore, "New", gatepolicy.PredicateRead, "")}, nil)
	// The underlying row still carries Old — the drop has not shipped yet — so
	// the one predicate still in force against it, the producer's own past read,
	// is satisfied by this candidate's own environment.
	g.storeState.rows[moving.ItemID+"/"+theStore] = []consumercontract.Document{{"Old": "x", "New": "y"}}
	checked, err := g.check.Enforce(ctx, moving, g.production)
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if checked.Passed() {
		t.Fatal("a move of reads to a new element with no backfill marked complete passed")
	}
	if len(checked.Migrations) != 1 || len(checked.Migrations[0].Waiting) != 1 ||
		!checked.Migrations[0].Waiting[0].Moving || checked.Migrations[0].Waiting[0].Element != "New" {
		t.Fatalf("the migration found is %+v, want New waiting and moving", checked.Migrations)
	}

	markBackfill(t, ctx, g, g.producer, theStore, "New", "Old")
	again := candidateOf(t, ctx, g, g.producer,
		[]contract.Form{stored(element("Old", "string", true, true), element("New", "string", false, false))},
		[]consumercontract.Draft{draft(g.producer, theStore, "New", gatepolicy.PredicateRead, "")}, nil)
	g.storeState.rows[again.ItemID+"/"+theStore] = []consumercontract.Document{{"Old": "x", "New": "y"}}
	checked, err = g.check.Enforce(ctx, again, g.production)
	if err != nil {
		t.Fatalf("the second Enforce: %v", err)
	}
	if !checked.Passed() {
		t.Fatalf("the move is still refused once the backfill is marked complete: %s", checked.Why())
	}
}

// TestASchemaChangeIsRejectedWhenAppliedTwiceChangesSomething: every change is
// authored to be applied twice without effect, because an engine that cannot put
// a change and its history row in one transaction can leave a change applied
// with no row.
func TestASchemaChangeIsRejectedWhenAppliedTwiceChangesSomething(t *testing.T) {
	ctx, g := newGraph(t)

	ship(t, ctx, g, g.producer, []contract.Form{stored(element("ID", "string", true, false))}, nil, window.ExitTimedOut)

	moved := candidateOf(t, ctx, g, g.producer,
		[]contract.Form{stored(element("ID", "string", true, false), element("Amount", "int64", false, false))}, nil, nil)
	g.storeState.appliedTwice[moved.ItemID] = contractcheck.SecondApplication{Ran: true, Changed: true, What: "a duplicate row"}

	checked, err := g.check.Enforce(ctx, moved, g.production)
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if checked.Passed() {
		t.Fatal("a schema change whose second application changed something passed")
	}
	if checked.Check() != gate.AutoRejectedByContractDiff {
		t.Errorf("the check that rejected is %q, want the store rule", checked.Check())
	}
	if !contains(checked.Why(), "a duplicate row") {
		t.Errorf("the rejection does not name what the second application changed: %s", checked.Why())
	}
}

// TestAStoreWhoseBuildDeclaresNoSchemaChangeIsNotBlocked: a store contract's
// form is derived from the code, so it moves whenever the code that derives it
// moves — and a build can move it and ship nothing for a deploy to apply. The
// double application is what the candidate environment does to a change, so it
// is asked for only where the build declares one; a candidate that declares
// none is not refused for a change it does not carry.
func TestAStoreWhoseBuildDeclaresNoSchemaChangeIsNotBlocked(t *testing.T) {
	ctx, g := newGraph(t)

	ship(t, ctx, g, g.producer, []contract.Form{stored(element("ID", "string", true, false))}, nil, window.ExitTimedOut)

	moved := candidateOf(t, ctx, g, g.producer,
		[]contract.Form{stored(element("ID", "string", true, false), element("Amount", "int64", false, false))}, nil, nil)
	g.checkout.noSchemaChange[moved.ItemID] = true

	checked, err := g.check.Enforce(ctx, moved, g.production)
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if !checked.Passed() {
		t.Fatalf("a candidate whose build declares no schema change is refused: %s", checked.Why())
	}
	if len(checked.Migrations) != 1 {
		t.Fatalf("the migrations found are %+v, want the one store contract", checked.Migrations)
	}
	if m := checked.Migrations[0]; !m.Moved || m.Declared || m.SecondApplication.Ran {
		t.Errorf("the migration reads %+v, want a moved form, no declared change, and no second application", m)
	}

	// The same form moved by a build that does declare one is refused where the
	// environment did not apply it twice, which is the rule this leaves standing.
	declared := candidateOf(t, ctx, g, g.producer,
		[]contract.Form{stored(element("ID", "string", true, false), element("Amount", "int64", false, false))}, nil, nil)
	g.storeState.appliedTwice[declared.ItemID] = contractcheck.SecondApplication{}
	checked, err = g.check.Enforce(ctx, declared, g.production)
	if err != nil {
		t.Fatalf("the second Enforce: %v", err)
	}
	if checked.Passed() {
		t.Fatal("a declared schema change the candidate environment never applied twice passed")
	}
	if !contains(checked.Why(), "did not apply the change twice") {
		t.Errorf("the rejection does not name the double application: %s", checked.Why())
	}
}

// TestADestructiveChangeIsRejectedWithNoSnapshot: the drop's diff destroys
// stored data, so the deploy it would rest on has to have taken and verified a
// snapshot first, and the candidate environment exercises the same thing.
func TestADestructiveChangeIsRejectedWithNoSnapshot(t *testing.T) {
	ctx, g := newGraph(t)

	ship(t, ctx, g, g.producer, []contract.Form{stored(element("ID", "string", true, true))}, nil, window.ExitTimedOut)
	markBackfill(t, ctx, g, g.producer, theStore, "ID", "ID_new")

	dropping := candidateOf(t, ctx, g, g.producer, []contract.Form{stored()}, nil, nil)
	g.storeState.snapshot[dropping.ItemID] = contractcheck.Snapshot{Taken: true, Verified: false, Why: "the disk was full"}

	checked, err := g.check.Enforce(ctx, dropping, g.production)
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if checked.Passed() {
		t.Fatal("a destructive change with an unverified snapshot passed")
	}
	if !contains(checked.Why(), "the disk was full") {
		t.Errorf("the rejection does not name why the snapshot did not verify: %s", checked.Why())
	}
}
