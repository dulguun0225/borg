// This file holds the tests of Decomposition's writes: creating an item,
// declaring what it waits on, and superseding one.
package item_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
)

func TestDecompositionWritesOnceAtSpec(t *testing.T) {
	ctx, pool, decomposition, _ := newWriters(t)

	it := oneItem(ctx, t, decomposition)
	if it.Stage != item.StageSpec {
		t.Errorf("a new item is at %s, want spec", it.Stage)
	}
	if _, err := time.Parse(record.TimeLayout, it.At); err != nil {
		t.Errorf("the item's timestamp %q: %v", it.At, err)
	}

	read, err := item.Get(ctx, pool, it.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(read, it) {
		t.Errorf("Get = %+v, want the item as decomposed, %+v", read, it)
	}

	if _, err := item.Get(ctx, pool, "it_missing"); !errors.Is(err, item.ErrNotFound) {
		t.Errorf("Get on a missing id = %v, want ErrNotFound", err)
	}
	if _, err := decomposition.Create(ctx, decompositionActor, item.New{IntentID: "in_x", ServiceID: "svc_x"}); !errors.Is(err, item.ErrBranchEmpty) {
		t.Errorf("Create with no branch = %v, want ErrBranchEmpty", err)
	}
	// An empty link names nothing, and the writer refuses it the way it
	// refuses every other required field. record's doc.go states what a link
	// is checked for.
	if _, err := decomposition.Create(ctx, decompositionActor, item.New{ServiceID: "svc_x", Branch: "item/x"}); !errors.Is(err, item.ErrIntentIDEmpty) {
		t.Errorf("Create naming no intent = %v, want ErrIntentIDEmpty", err)
	}
	if _, err := decomposition.Create(ctx, decompositionActor, item.New{IntentID: "in_x", Branch: "item/x"}); !errors.Is(err, item.ErrServiceIDEmpty) {
		t.Errorf("Create naming no service = %v, want ErrServiceIDEmpty", err)
	}
	if _, err := decomposition.Create(ctx, record.Actor{}, item.New{IntentID: "in_x", ServiceID: "svc_x", Branch: "item/x"}); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("Create with no actor = %v, want record.ErrKindUnknown", err)
	}
}

// TestDecompositionDeclaresWhatAnItemWaitsOn: the decomposition records the order, so a dependency
// is declared there and not discovered at deploy time. It reads back as the ids
// the decomposition named, and an empty one is refused.
func TestDecompositionDeclaresWhatAnItemWaitsOn(t *testing.T) {
	ctx, pool, decomposition, _ := newWriters(t)

	waits := []string{"it_" + strings.Repeat("a", 32), "it_" + strings.Repeat("b", 32)}
	it, err := decomposition.Create(ctx, decompositionActor, item.New{
		IntentID:  "in_" + strings.Repeat("0", 32),
		ServiceID: "svc_" + strings.Repeat("0", 32),
		Branch:    "item/dependent",
		WaitsOn:   waits,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	read, err := item.Get(ctx, pool, it.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(read.WaitsOn, waits) {
		t.Errorf("the item waits on %v, decomposition declared %v", read.WaitsOn, waits)
	}
	if read.Priority != 0 {
		t.Errorf("a freshly decomposed item has priority %d, decomposition writes nothing", read.Priority)
	}

	if _, err := decomposition.Create(ctx, decompositionActor, item.New{
		IntentID: "in_x", ServiceID: "svc_x", Branch: "item/x", WaitsOn: []string{""},
	}); !errors.Is(err, item.ErrItemIDEmpty) {
		t.Errorf("Create waiting on an empty id = %v, want ErrItemIDEmpty", err)
	}
}

// TestSupersedeEndsAnItemAndPointsItAtWhatReplacedIt: the decomposition's second write and its
// only write to an existing item. A rejected set is superseded rather than discarded,
// so what was decomposed wrong is readable beside what replaced it.
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

	merged := oneItem(ctx, t, decomposition)
	for _, stage := range []item.Stage{item.StageImplementation, item.StageQueued, item.StageMerged} {
		if _, err := dispatch.Advance(ctx, dispatchActor, merged.ID, stage); err != nil {
			t.Fatalf("advancing to %s: %v", stage, err)
		}
	}
	if _, err := decomposition.Supersede(ctx, decompositionActor, merged.ID, nil); !errors.Is(err, item.ErrMerged) {
		t.Errorf("superseding a merged item = %v, want ErrMerged", err)
	}
}
