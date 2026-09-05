// Tests of the three items that get a breaking change through: the
// addition, the consumer's migration, and the removal once nobody reads
// the old element.
package main

import (
	"testing"

	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/service"
)

// TestTheThreeItemsOfAMigrationGetTheBreakingChangeThrough: the addition, the
// consumer's migration, and the removal — each shipping alone, and the removal
// passing the same check that rejected the second episode because the list emptied
// with nobody removing anything.
func TestTheThreeItemsOfAMigrationGetTheBreakingChangeThrough(t *testing.T) {
	ctx, d, out := newContractPath(t)
	migrated(t, ctx, d, out)

	// The detector raised the removal when the list emptied, which is the third
	// item's intent and nobody had to remember it. It is found by the evidence
	// the detector keys it by — the contract and the element — the way
	// [contractcheck.Check.RaiseRemovals] itself attaches to an intent already
	// waiting rather than by the statement's text, which package intent's
	// rewrite no longer offers a way to look up by.
	producer, found, err := service.ByName(ctx, d.pool, theService)
	if err != nil || !found {
		t.Fatalf("reading the producer: found %v, %v", found, err)
	}
	con, found, err := contract.ByName(ctx, d.pool, producer.ID, theHealthInterface)
	if err != nil || !found {
		t.Fatalf("reading the contract: found %v, %v", found, err)
	}
	waiting, found, err := intent.OnEvidence(ctx, d.pool, intent.Evidence{ContractID: con.ID, Element: "Detail"})
	if err != nil {
		t.Fatalf("reading the detector's intent: %v", err)
	}
	if !found {
		t.Fatalf("the detector raised no removal intent after the list emptied:\n%s", out)
	}
	if waiting.Source != intent.SourceDetector {
		t.Errorf("the removal intent came from %s, want the detector", waiting.Source)
	}

	// `take` no longer resumes the intent already waiting by matching the
	// statement's own text — package intent's rewrite drops the lookup that let
	// it, [authorintent.go]'s own comment says so — so this run takes a second
	// intent in for the same words rather than continuing the detector's one.
	// What still matters is that the removal itself gets through: it passes the
	// same check that rejected episode two and publishes the major.
	removed := only(t, runOne(t, ctx, d, out, removeStatement, theService))
	if removed.intentID == waiting.ID {
		t.Errorf("the removal run resumed the detector's intent %s; that lookup is not built at this milestone, so a fresh one was expected", waiting.ID)
	}
	if !removed.merged {
		t.Fatalf("the removal did not merge after the list emptied:\n%s", out)
	}
	if len(removed.published) != 1 || removed.published[0].Version.Semver != (contract.Semver{Major: 2}) {
		t.Fatalf("the removal published %+v, want 2.0.0 — a removal is the major", removed.published)
	}
	if len(removed.published[0].Change.Removed) != 1 || removed.published[0].Change.Removed[0] != "Detail" {
		t.Errorf("the removal's diff removed %v", removed.published[0].Change.Removed)
	}
}
