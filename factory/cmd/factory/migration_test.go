// Tests of the three items that get a breaking change through: the
// addition, the consumer's migration, and the removal once nobody reads
// the old element.
package main

import (
	"testing"

	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/intent"
)

// TestTheThreeItemsOfAMigrationGetTheBreakingChangeThrough: the addition, the
// consumer's migration, and the removal — each shipping alone, and the removal
// passing the same check that rejected the second episode because the list emptied
// with nobody removing anything.
func TestTheThreeItemsOfAMigrationGetTheBreakingChangeThrough(t *testing.T) {
	ctx, d, out := newContractPath(t)
	migrated(t, ctx, d, out)

	// The detector raised the removal when the list emptied, which is the third
	// item's intent and nobody had to remember it.
	waiting, found, err := intent.Unrefined(ctx, d.pool, removeStatement)
	if err != nil {
		t.Fatalf("reading the detector's intent: %v", err)
	}
	if !found {
		t.Fatalf("the detector raised no removal intent after the list emptied:\n%s", out)
	}
	if waiting.Source != intent.SourceDetector {
		t.Errorf("the removal intent came from %s, want the detector", waiting.Source)
	}

	removed := only(t, runOne(t, ctx, d, out, removeStatement, theService))
	if removed.intentID != waiting.ID {
		t.Fatalf("the removal run took in intent %s, and the detector's is %s — a run given a statement works the intent waiting with it",
			removed.intentID, waiting.ID)
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
