// This file holds the tests of superseding an item: the pointer at what
// replaced it, the write that creates the replacements and the pointer at
// once, and what a supersede is refused on.
package item_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/item"
)

// TestSupersedeEndsAnItemAndPointsItAtWhatReplacedIt: the decomposition's write
// to an existing item. A rejected set is superseded rather than discarded, so
// what was decomposed wrong is readable beside what replaced it.
func TestSupersedeEndsAnItemAndPointsItAtWhatReplacedIt(t *testing.T) {
	ctx, pool, decomposition, _ := newWriters(t)

	replaced := oneItem(ctx, t, decomposition)
	first := oneItem(ctx, t, decomposition)
	second := oneItem(ctx, t, decomposition)

	ended, err := decomposition.Supersede(ctx, decompositionActor, replaced.ID, []string{first.ID, second.ID})
	if err != nil {
		t.Fatalf("Supersede: %v", err)
	}
	if ended.Stage != item.StageSuperseded {
		t.Fatalf("the superseded item is at %s", ended.Stage)
	}
	read, err := item.Get(ctx, pool, replaced.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Stage != item.StageSuperseded {
		t.Errorf("the stored stage is %s", read.Stage)
	}
	if len(read.SupersededBy) != 2 || read.SupersededBy[0] != first.ID || read.SupersededBy[1] != second.ID {
		t.Fatalf("the item points at %v, want the two that replaced it", read.SupersededBy)
	}

	// A re-decomposition that replaced an item with nothing leaves the pointer unwritten, and
	// what says why is the superseded stage beside the decision that rejected the set.
	dropped := oneItem(ctx, t, decomposition)
	if _, err := decomposition.Supersede(ctx, decompositionActor, dropped.ID, nil); err != nil {
		t.Fatalf("superseding with no replacement: %v", err)
	}
	read, err = item.Get(ctx, pool, dropped.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(read.SupersededBy) != 0 || read.Stage != item.StageSuperseded {
		t.Errorf("the dropped item reads back as %+v", read)
	}
}

// TestSupersedingTwiceOrSupersedingAMergedItemIsRefused: superseding does not run
// twice, and a merged item is out of a re-decomposition's reach.
func TestSupersedingTwiceOrSupersedingAMergedItemIsRefused(t *testing.T) {
	ctx, _, decomposition, dispatch := newWriters(t)

	once := oneItem(ctx, t, decomposition)
	if _, err := decomposition.Supersede(ctx, decompositionActor, once.ID, nil); err != nil {
		t.Fatalf("the first Supersede: %v", err)
	}
	if _, err := decomposition.Supersede(ctx, decompositionActor, once.ID, nil); !errors.Is(err, item.ErrAlreadySuperseded) {
		t.Errorf("superseding twice = %v, want ErrAlreadySuperseded", err)
	}

	merged := advanceToQueued(ctx, t, decomposition, dispatch)
	if _, err := dispatch.End(ctx, dispatchActor, merged.ID); err != nil {
		t.Fatalf("End: %v", err)
	}
	if _, err := decomposition.Supersede(ctx, decompositionActor, merged.ID, nil); !errors.Is(err, item.ErrMerged) {
		t.Errorf("superseding a merged item = %v, want ErrMerged", err)
	}
}

// TestSupersedeTxWritesThePointerInTheWriteThatCreatesTheReplacements: each
// rejected item points at the items of the re-decomposition that replaced it,
// written in the same write that creates them, so what was decomposed wrong is
// readable beside what replaced it from the moment either row exists.
func TestSupersedeTxWritesThePointerInTheWriteThatCreatesTheReplacements(t *testing.T) {
	ctx, pool, decomposition, _ := newWriters(t)
	replaced := oneItem(ctx, t, decomposition)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	answers := []string{"rq_" + strings.Repeat("s", 32)}
	var replacements []string
	for _, branch := range []string{"item/first", "item/second"} {
		made, err := decomposition.CreateTx(ctx, tx, decompositionActor, item.New{
			IntentID: "in_x", ServiceID: "svc_x", Branch: branch, RequirementsAnswered: answers,
		}, "", "")
		if err != nil {
			t.Fatalf("CreateTx %s: %v", branch, err)
		}
		replacements = append(replacements, made.ID)
	}
	ended, err := decomposition.SupersedeTx(ctx, tx, decompositionActor, replaced.ID, replacements)
	if err != nil {
		t.Fatalf("SupersedeTx: %v", err)
	}
	if ended.Stage != item.StageSuperseded {
		t.Errorf("SupersedeTx returned stage %s, want superseded", ended.Stage)
	}

	// Until the transaction commits neither the replacements nor the pointer is
	// there, which is what one write means.
	if standing, err := item.Get(ctx, pool, replaced.ID); err != nil || standing.Stage != item.StageSpec {
		t.Errorf("before the commit the replaced item reads %+v, %v, want it still at spec", standing, err)
	}
	if _, err := item.Get(ctx, pool, replacements[0]); !errors.Is(err, item.ErrNotFound) {
		t.Errorf("before the commit a replacement reads %v, want ErrNotFound", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	read, err := item.Get(ctx, pool, replaced.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Stage != item.StageSuperseded || len(read.SupersededBy) != 2 ||
		read.SupersededBy[0] != replacements[0] || read.SupersededBy[1] != replacements[1] {
		t.Errorf("after the commit the replaced item reads %+v, want superseded and pointing at both replacements", read)
	}
	for _, id := range replacements {
		if _, err := item.Get(ctx, pool, id); err != nil {
			t.Errorf("the replacement %s: %v", id, err)
		}
	}
}
