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

// TestAnItemAnsweringNoRequirementIsRefused: every item decomposition writes
// answers a requirement whole or carries a derived share of one, the item that
// creates a service and each step of a migration included. An item answering
// none is work nobody asked for, whose criteria at Spec would trace to
// nothing.
func TestAnItemAnsweringNoRequirementIsRefused(t *testing.T) {
	ctx, _, decomposition, _ := newWriters(t)

	none := item.New{IntentID: "in_x", ServiceID: "svc_x", Branch: "item/unasked"}
	if _, err := decomposition.Create(ctx, decompositionActor, none, "", "", nil); !errors.Is(err, item.ErrAnswersNoRequirement) {
		t.Errorf("Create answering no requirement = %v, want ErrAnswersNoRequirement", err)
	}
	answering := none
	answering.RequirementsAnswered = []string{"rq_" + strings.Repeat("c", 32)}
	if _, err := decomposition.Create(ctx, decompositionActor, answering, "", "", nil); err != nil {
		t.Errorf("Create answering one requirement: %v", err)
	}
}

// TestAnAreaOutsideTheServicesProjectIsRefused: decomposition writes only an
// area inside the project of the service the item names, so the item's area and
// its service agree by construction. The two projects are the caller's to read
// — an area chain is package area's and a service's project is package
// service's — and this is where they are compared.
func TestAnAreaOutsideTheServicesProjectIsRefused(t *testing.T) {
	ctx, _, decomposition, _ := newWriters(t)

	answers := []string{"rq_" + strings.Repeat("d", 32)}
	n := item.New{IntentID: "in_x", ServiceID: "svc_x", AreaChain: []string{"ar_x"}, Branch: "item/x",
		RequirementsAnswered: answers}
	if _, err := decomposition.Create(ctx, decompositionActor, n, "pr_a", "pr_b", nil); !errors.Is(err, item.ErrAreaOutsideServiceProject) {
		t.Errorf("Create with the area in another project = %v, want ErrAreaOutsideServiceProject", err)
	}
	if _, err := decomposition.Create(ctx, decompositionActor, n, "pr_a", "pr_a", nil); err != nil {
		t.Errorf("Create with the area inside the service's project: %v", err)
	}

	// An item may name no area, and then there is no project to compare.
	noArea := item.New{IntentID: "in_x", ServiceID: "svc_x", Branch: "item/y", RequirementsAnswered: answers}
	if _, err := decomposition.Create(ctx, decompositionActor, noArea, "", "pr_b", nil); err != nil {
		t.Errorf("Create with no area = %v, want no comparison at all", err)
	}
	if _, err := decomposition.Create(ctx, decompositionActor,
		item.New{IntentID: "in_x", ServiceID: "svc_x", AreaChain: []string{""}, Branch: "item/z",
			RequirementsAnswered: answers}, "pr_a", "pr_a", nil); !errors.Is(err, item.ErrAreaIDEmpty) {
		t.Errorf("Create with an empty area in the chain = %v, want ErrAreaIDEmpty", err)
	}
}

// TestTheAreaWrittenIsTheNarrowestOfTheChain: decomposition writes the
// narrowest area in the chain whose declaration covers the work, and the chain
// arrives narrowest first because walking it is package area's. Which area the
// item names is decided here and is not whatever the caller passes.
func TestTheAreaWrittenIsTheNarrowestOfTheChain(t *testing.T) {
	ctx, pool, decomposition, _ := newWriters(t)

	narrowest := "ar_" + strings.Repeat("1", 32)
	chain := []string{narrowest, "ar_" + strings.Repeat("2", 32), "ar_" + strings.Repeat("3", 32)}
	it, err := decomposition.Create(ctx, decompositionActor, item.New{
		IntentID: "in_x", ServiceID: "svc_x", AreaChain: chain, Branch: "item/narrow",
		RequirementsAnswered: []string{"rq_" + strings.Repeat("e", 32)},
	}, oneProject, oneProject, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if it.AreaID != narrowest {
		t.Errorf("the item names area %s, want the narrowest of the chain %s", it.AreaID, narrowest)
	}
	read, err := item.Get(ctx, pool, it.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.AreaID != narrowest {
		t.Errorf("the stored area is %s, want the narrowest of the chain %s", read.AreaID, narrowest)
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

// TestCreateWritesTheItemUnderTheIdTheCallerMinted: a caller that has to write
// a record naming the item before the item exists mints the id with
// [item.NewID] and passes it, and the row is written under exactly that id. It
// is what decomposition does for a split's derived requirements, each of which
// names the item that answers it while the item answers the share.
func TestCreateWritesTheItemUnderTheIdTheCallerMinted(t *testing.T) {
	ctx, pool, decomposition, _ := newWriters(t)

	minted := item.NewID()
	it, err := decomposition.Create(ctx, decompositionActor, item.New{
		ID:                   minted,
		IntentID:             "in_" + strings.Repeat("0", 32),
		ServiceID:            "svc_" + strings.Repeat("0", 32),
		Branch:               "item/minted",
		RequirementsAnswered: []string{"rq_" + strings.Repeat("0", 32)},
	}, "", "", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if it.ID != minted {
		t.Errorf("Create wrote %s, the caller minted %s", it.ID, minted)
	}
	if _, err := item.Get(ctx, pool, minted); err != nil {
		t.Errorf("Get of the minted id: %v", err)
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
		RequirementsAnswered: []string{"rq_" + strings.Repeat("f", 32)},
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
// refused where the items are kept, naming the edge that closes it.
func TestAWriteThatWouldCloseACycleIsRefused(t *testing.T) {
	ctx, _, decomposition, _ := newWriters(t)

	first := oneItem(ctx, t, decomposition)
	second, err := decomposition.Create(ctx, decompositionActor, item.New{
		IntentID: "in_x", ServiceID: "svc_x", Branch: "item/second", WaitsOn: []string{first.ID},
		RequirementsAnswered: []string{"rq_" + strings.Repeat("g", 32)},
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
}

// TestTheHoldsEdgesAreComputedHereAndNamedInTheRefusal: while a rollback hold
// stands on a service, every unmerged item of that service other than the
// revert waits on the revert item, and no record holds those edges. The caller
// names the service and the intent the revert was decomposed from, which is
// what the production deploy gate reads the hold from, and the edges are
// computed at the write against the items standing then. What they refuse is a
// revert declaring a dependency on a sibling its own hold holds, and the
// refusal names the hold, nothing having declared the edge that closed the
// cycle.
func TestTheHoldsEdgesAreComputedHereAndNamedInTheRefusal(t *testing.T) {
	ctx, _, decomposition, dispatch := newWriters(t)

	const held = "svc_" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const revertIntent = "in_" + "rrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrr"
	answers := []string{"rq_" + strings.Repeat("h", 32)}
	sibling, err := decomposition.Create(ctx, decompositionActor, item.New{
		IntentID: "in_x", ServiceID: held, Branch: "item/sibling", RequirementsAnswered: answers,
	}, "", "", nil)
	if err != nil {
		t.Fatalf("Create the sibling: %v", err)
	}
	hold := []item.Hold{{ServiceID: held, RevertIntentID: revertIntent}}

	// The revert is decomposed while its own hold stands, so the hold's edges
	// lead into an item that is not in the table until this write lands.
	_, err = decomposition.Create(ctx, decompositionActor, item.New{
		IntentID: revertIntent, ServiceID: held, Branch: "item/revert", WaitsOn: []string{sibling.ID},
		RequirementsAnswered: answers,
	}, "", "", hold)
	if !errors.Is(err, item.ErrWouldCloseACycle) {
		t.Fatalf("a revert waiting on a sibling its own hold holds = %v, want ErrWouldCloseACycle", err)
	}
	if !strings.Contains(err.Error(), held) || !strings.Contains(err.Error(), revertIntent) {
		t.Errorf("the refusal is %q, and it names the hold neither by its service nor by its revert", err)
	}

	// Without the hold the same declaration is an ordinary dependency, which is
	// what says the refusal came from the edges the hold imposes.
	revert, err := decomposition.Create(ctx, decompositionActor, item.New{
		IntentID: revertIntent, ServiceID: held, Branch: "item/revert", WaitsOn: []string{sibling.ID},
		RequirementsAnswered: answers,
	}, "", "", nil)
	if err != nil {
		t.Fatalf("the same declaration without the hold: %v", err)
	}

	// An item of another service is no part of this hold, and one whose work is
	// over is out of the graph: neither is an edge, so a wait on the revert
	// closes no cycle through them.
	elsewhere, err := decomposition.Create(ctx, decompositionActor, item.New{
		IntentID: "in_x", ServiceID: "svc_y", Branch: "item/elsewhere", RequirementsAnswered: answers,
	}, "", "", nil)
	if err != nil {
		t.Fatalf("Create the item elsewhere: %v", err)
	}
	if _, err := decomposition.Repoint(ctx, decompositionActor, elsewhere.ID, []string{revert.ID}, hold); err != nil {
		t.Errorf("an item of another service waiting on the revert = %v, want no refusal", err)
	}
	if _, err := dispatch.Drop(ctx, workActor, sibling.ID); err != nil {
		t.Fatalf("Drop the sibling: %v", err)
	}
	if _, err := decomposition.Repoint(ctx, decompositionActor, revert.ID, []string{sibling.ID}, hold); err != nil {
		t.Errorf("the revert waiting on an item the hold no longer reaches = %v, want no refusal", err)
	}

	if _, err := decomposition.Repoint(ctx, decompositionActor, revert.ID, nil,
		[]item.Hold{{ServiceID: held}}); !errors.Is(err, item.ErrHoldIncomplete) {
		t.Errorf("a hold naming no revert intent = %v, want ErrHoldIncomplete", err)
	}
}

// TestACycleThroughTwoHoldsNamesBoth: two reverts each declaring a dependency
// on an item the other's hold holds is the second thing the union refuses, and
// it runs through the holds of two services, neither of them the one the write
// names. Both are named in the refusal, nothing having declared either edge.
func TestACycleThroughTwoHoldsNamesBoth(t *testing.T) {
	ctx, _, decomposition, _ := newWriters(t)

	const heldA, heldB = "svc_" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "svc_" + "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const revertA, revertB = "in_" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "in_" + "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	holds := []item.Hold{{ServiceID: heldA, RevertIntentID: revertA}, {ServiceID: heldB, RevertIntentID: revertB}}

	siblingA := itemOn(ctx, t, decomposition, "in_ordinary", heldA, "item/sibling-a")
	siblingB := itemOn(ctx, t, decomposition, "in_ordinary", heldB, "item/sibling-b")
	// The first revert waits on the other service's sibling, which is an
	// ordinary declared dependency and closes nothing on its own.
	if _, err := decomposition.Create(ctx, decompositionActor, item.New{
		IntentID: revertA, ServiceID: heldA, Branch: "item/revert-a", WaitsOn: []string{siblingB.ID},
		RequirementsAnswered: []string{"rq_" + strings.Repeat("i", 32)},
	}, "", "", holds); err != nil {
		t.Fatalf("decomposing the first revert: %v", err)
	}
	second := itemOn(ctx, t, decomposition, revertB, heldB, "item/revert-b")

	_, err := decomposition.Repoint(ctx, decompositionActor, second.ID, []string{siblingA.ID}, holds)
	if !errors.Is(err, item.ErrWouldCloseACycle) {
		t.Fatalf("the second revert waiting on what the first hold holds = %v, want ErrWouldCloseACycle", err)
	}
	for _, named := range []string{heldA, heldB, revertA, revertB} {
		if !strings.Contains(err.Error(), named) {
			t.Errorf("the refusal is %q, and it does not name %s", err, named)
		}
	}
}
