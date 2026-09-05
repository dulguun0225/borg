// This file holds the tests of Decomposition's writes: creating an item,
// declaring what it waits on and what it answers, repointing a standing item,
// the refusal of a write that would close a cycle, and superseding one.
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

	// Spec is entered to author the moment the item exists, so the count for it
	// stands at one and not at nothing.
	stages, err := item.Stages(ctx, pool, it.ID)
	if err != nil {
		t.Fatalf("Stages: %v", err)
	}
	if len(stages) != 1 || stages[0].Stage != item.StageSpec || stages[0].Attempts != 1 {
		t.Errorf("a new item's stage rows are %+v, want one attempt at spec", stages)
	}

	if _, err := item.Get(ctx, pool, "it_missing"); !errors.Is(err, item.ErrNotFound) {
		t.Errorf("Get on a missing id = %v, want ErrNotFound", err)
	}
	if _, err := decomposition.Create(ctx, decompositionActor,
		item.New{IntentID: "in_x", ServiceID: "svc_x"}, "", "", nil); !errors.Is(err, item.ErrBranchEmpty) {
		t.Errorf("Create with no branch = %v, want ErrBranchEmpty", err)
	}
	// An empty link names nothing, and the writer refuses it the way it
	// refuses every other required field. record's doc.go states what a link
	// is checked for.
	if _, err := decomposition.Create(ctx, decompositionActor,
		item.New{ServiceID: "svc_x", Branch: "item/x"}, "", "", nil); !errors.Is(err, item.ErrIntentIDEmpty) {
		t.Errorf("Create naming no intent = %v, want ErrIntentIDEmpty", err)
	}
	if _, err := decomposition.Create(ctx, decompositionActor,
		item.New{IntentID: "in_x", Branch: "item/x"}, "", "", nil); !errors.Is(err, item.ErrServiceIDEmpty) {
		t.Errorf("Create naming no service = %v, want ErrServiceIDEmpty", err)
	}
	if _, err := decomposition.Create(ctx, record.Actor{},
		item.New{IntentID: "in_x", ServiceID: "svc_x", Branch: "item/x"}, "", "", nil); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("Create with no actor = %v, want record.ErrKindUnknown", err)
	}
}

// TestAnAreaOutsideTheServicesProjectIsRefused: decomposition writes only an
// area inside the project of the service the item names, so the item's area and
// its service agree by construction. The two projects are the caller's to read
// — an area chain is package area's and a service's project is package
// service's — and this is where they are compared.
func TestAnAreaOutsideTheServicesProjectIsRefused(t *testing.T) {
	ctx, _, decomposition, _ := newWriters(t)

	n := item.New{IntentID: "in_x", ServiceID: "svc_x", AreaID: "ar_x", Branch: "item/x"}
	if _, err := decomposition.Create(ctx, decompositionActor, n, "pr_a", "pr_b", nil); !errors.Is(err, item.ErrAreaOutsideServiceProject) {
		t.Errorf("Create with the area in another project = %v, want ErrAreaOutsideServiceProject", err)
	}
	if _, err := decomposition.Create(ctx, decompositionActor, n, "pr_a", "pr_a", nil); err != nil {
		t.Errorf("Create with the area inside the service's project: %v", err)
	}

	// An item may name no area, and then there is no project to compare.
	noArea := item.New{IntentID: "in_x", ServiceID: "svc_x", Branch: "item/y"}
	if _, err := decomposition.Create(ctx, decompositionActor, noArea, "", "pr_b", nil); err != nil {
		t.Errorf("Create with no area = %v, want no comparison at all", err)
	}
}

// TestDecompositionDeclaresWhatAnItemWaitsOnAndAnswers: the decomposition
// records the order and which of the intent's requirements each item answers,
// so both are declared there and not discovered later. They read back as the
// ids decomposition named, and an empty one is refused.
func TestDecompositionDeclaresWhatAnItemWaitsOnAndAnswers(t *testing.T) {
	ctx, pool, decomposition, _ := newWriters(t)

	first := oneItem(ctx, t, decomposition)
	second := oneItem(ctx, t, decomposition)
	waits := []string{first.ID, second.ID}
	answers := []string{"rq_" + strings.Repeat("a", 32), "rq_" + strings.Repeat("b", 32)}
	it, err := decomposition.Create(ctx, decompositionActor, item.New{
		IntentID:             "in_" + strings.Repeat("0", 32),
		ServiceID:            "svc_" + strings.Repeat("0", 32),
		Branch:               "item/dependent",
		WaitsOn:              waits,
		RequirementsAnswered: answers,
	}, "", "", nil)
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
	if !reflect.DeepEqual(read.RequirementsAnswered, answers) {
		t.Errorf("the item answers %v, decomposition declared %v", read.RequirementsAnswered, answers)
	}
	if read.Priority != 0 {
		t.Errorf("a freshly decomposed item has priority %d, decomposition writes nothing", read.Priority)
	}

	if _, err := decomposition.Create(ctx, decompositionActor, item.New{
		IntentID: "in_x", ServiceID: "svc_x", Branch: "item/x", WaitsOn: []string{""},
	}, "", "", nil); !errors.Is(err, item.ErrItemIDEmpty) {
		t.Errorf("Create waiting on an empty id = %v, want ErrItemIDEmpty", err)
	}
	if _, err := decomposition.Create(ctx, decompositionActor, item.New{
		IntentID: "in_x", ServiceID: "svc_x", Branch: "item/x", RequirementsAnswered: []string{""},
	}, "", "", nil); !errors.Is(err, item.ErrRequirementIDEmpty) {
		t.Errorf("Create answering an empty requirement id = %v, want ErrRequirementIDEmpty", err)
	}
}

// TestRepointMovesAStandingItemsWaitToTheReplacements: a re-decomposition
// points what waited on a superseded item at the items that replaced it, which
// is the inverse of the pointer the superseded item carries. An ended item
// waits on nothing and is refused.
func TestRepointMovesAStandingItemsWaitToTheReplacements(t *testing.T) {
	ctx, pool, decomposition, dispatch := newWriters(t)

	replaced := oneItem(ctx, t, decomposition)
	standing, err := decomposition.Create(ctx, decompositionActor, item.New{
		IntentID: "in_x", ServiceID: "svc_x", Branch: "item/standing", WaitsOn: []string{replaced.ID},
	}, "", "", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	first := oneItem(ctx, t, decomposition)
	second := oneItem(ctx, t, decomposition)
	if _, err := decomposition.Supersede(ctx, decompositionActor, replaced.ID, []string{first.ID, second.ID}); err != nil {
		t.Fatalf("Supersede: %v", err)
	}
	repointed, err := decomposition.Repoint(ctx, decompositionActor, standing.ID, []string{first.ID, second.ID}, nil)
	if err != nil {
		t.Fatalf("Repoint: %v", err)
	}
	if !reflect.DeepEqual(repointed.WaitsOn, []string{first.ID, second.ID}) {
		t.Errorf("Repoint returned %v, want the two replacements", repointed.WaitsOn)
	}
	read, err := item.Get(ctx, pool, standing.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(read.WaitsOn, []string{first.ID, second.ID}) {
		t.Errorf("the stored waits are %v, want the two replacements", read.WaitsOn)
	}

	if _, err := decomposition.Repoint(ctx, decompositionActor, standing.ID, []string{""}, nil); !errors.Is(err, item.ErrItemIDEmpty) {
		t.Errorf("Repoint onto an empty id = %v, want ErrItemIDEmpty", err)
	}
	if _, err := decomposition.Repoint(ctx, decompositionActor, "it_missing", nil, nil); !errors.Is(err, item.ErrNotFound) {
		t.Errorf("Repoint on a missing item = %v, want ErrNotFound", err)
	}
	if _, err := dispatch.Drop(ctx, workActor, standing.ID); err != nil {
		t.Fatalf("Drop: %v", err)
	}
	if _, err := decomposition.Repoint(ctx, decompositionActor, standing.ID, nil, nil); !errors.Is(err, item.ErrEnded) {
		t.Errorf("Repoint on a dropped item = %v, want ErrEnded", err)
	}
}

// TestAWriteThatWouldCloseACycleIsRefused: two items each holding a deploy gate
// on the other is a wait nothing lifts and no instrument shows, so the write is
// refused where the items are kept, naming the edge that closes it. The
// relation checked is the union of what decomposition declared and what a
// rollback hold imposes, which no record holds and the caller passes in.
func TestAWriteThatWouldCloseACycleIsRefused(t *testing.T) {
	ctx, _, decomposition, _ := newWriters(t)

	first := oneItem(ctx, t, decomposition)
	second, err := decomposition.Create(ctx, decompositionActor, item.New{
		IntentID: "in_x", ServiceID: "svc_x", Branch: "item/second", WaitsOn: []string{first.ID},
	}, "", "", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	err = func() error {
		_, err := decomposition.Repoint(ctx, decompositionActor, first.ID, []string{second.ID}, nil)
		return err
	}()
	if !errors.Is(err, item.ErrWouldCloseACycle) {
		t.Errorf("repointing the first at the second = %v, want ErrWouldCloseACycle", err)
	}
	if !strings.Contains(err.Error(), first.ID) || !strings.Contains(err.Error(), second.ID) {
		t.Errorf("the refusal is %q, and it names neither end of the edge that closes the cycle", err)
	}

	// The edges a rollback hold imposes are held by no record, so the caller
	// passes them in and the check is over the union. A revert declaring a
	// dependency on a sibling its own hold holds is what that refuses. The
	// revert's own id is not known before it is created — [Decomposition.Create]
	// mints it internally — so the hold naming it is exercised through
	// [Decomposition.Repoint] instead, over the id Create actually returned.
	sibling := oneItem(ctx, t, decomposition)
	revert, err := decomposition.Create(ctx, decompositionActor, item.New{
		IntentID: "in_x", ServiceID: "svc_x", Branch: "item/revert",
	}, "", "", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	hold := []item.Edge{{From: sibling.ID, To: revert.ID}}
	_, err = decomposition.Repoint(ctx, decompositionActor, revert.ID, []string{sibling.ID}, hold)
	if !errors.Is(err, item.ErrWouldCloseACycle) {
		t.Errorf("a revert waiting on a sibling its hold holds = %v, want ErrWouldCloseACycle", err)
	}

	// Without the hold's edges the same declaration is an ordinary dependency.
	if _, err := decomposition.Repoint(ctx, decompositionActor, revert.ID, []string{sibling.ID}, nil); err != nil {
		t.Errorf("the same declaration without the hold = %v, want no refusal", err)
	}
}

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
