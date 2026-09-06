// This file holds the tests of Dispatch's writes: advancing a stage and
// counting the entry, sending one back without counting, merging, escalating,
// clearing an escalation, dropping, and setting priority.
package item_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
)

// advanceToQueued walks one item from spec to queued, which is as far as
// Advance goes: merged is End's.
func advanceToQueued(ctx context.Context, t *testing.T, decomposition *item.Decomposition, dispatch *item.Dispatch) item.Item {
	t.Helper()
	it := oneItem(ctx, t, decomposition)
	for _, stage := range []item.Stage{
		item.StageImplementationPlan, item.StageTasks, item.StageImplementation, item.StageQueued,
	} {
		if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, stage); err != nil {
			t.Fatalf("Advance to %s: %v", stage, err)
		}
	}
	return it
}

// TestAdvanceMovesOneStageForwardAndCountsTheEntry: an attempt is counted when
// a stage is entered to author, so each authoring stage stands at one after the
// item has passed through it once, and queued — which nothing authors —
// counts nothing.
func TestAdvanceMovesOneStageForwardAndCountsTheEntry(t *testing.T) {
	ctx, pool, decomposition, dispatch := newWriters(t)
	it := oneItem(ctx, t, decomposition)

	advanced, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageImplementationPlan)
	if err != nil {
		t.Fatalf("Advance to the implementation plan: %v", err)
	}
	if advanced.Stage != item.StageImplementationPlan {
		t.Errorf("Advance returned stage %s, want implementation_plan", advanced.Stage)
	}
	// The advance rewrites the stage and nothing else.
	advanced.Stage = it.Stage
	if !reflect.DeepEqual(advanced, it) {
		t.Errorf("Advance rewrote more than the stage: %+v, decomposed as %+v", advanced, it)
	}

	for _, stage := range []item.Stage{item.StageTasks, item.StageImplementation, item.StageQueued} {
		if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, stage); err != nil {
			t.Fatalf("Advance to %s: %v", stage, err)
		}
	}
	read, err := item.Get(ctx, pool, it.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Stage != item.StageQueued {
		t.Errorf("the item is at %s, want queued", read.Stage)
	}

	stages, err := item.Stages(ctx, pool, it.ID)
	if err != nil {
		t.Fatalf("Stages: %v", err)
	}
	if len(stages) != len(item.AuthoringStages) {
		t.Fatalf("Stages returned %+v, want one row per authoring stage and none for queued", stages)
	}
	for _, s := range stages {
		if s.Attempts != 1 || s.AttemptsSinceCleared() != 1 {
			t.Errorf("%s stands at %d attempts, want the one entry it was given", s.Stage, s.Attempts)
		}
	}

	// Merged is End's and never an advance, so queued has no next stage.
	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageMerged); !errors.Is(err, item.ErrNotNextStage) {
		t.Errorf("Advance to merged = %v, want ErrNotNextStage", err)
	}
}

func TestAdvanceRefusesSkipsAndBackwardsMoves(t *testing.T) {
	ctx, _, decomposition, dispatch := newWriters(t)
	it := oneItem(ctx, t, decomposition)

	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageTasks); !errors.Is(err, item.ErrNotNextStage) {
		t.Errorf("Advance skipping the implementation plan = %v, want ErrNotNextStage", err)
	}
	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageSpec); !errors.Is(err, item.ErrNotNextStage) {
		t.Errorf("Advance to the stage it is at = %v, want ErrNotNextStage", err)
	}

	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageImplementationPlan); err != nil {
		t.Fatalf("Advance to the implementation plan: %v", err)
	}
	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageSpec); !errors.Is(err, item.ErrNotNextStage) {
		t.Errorf("Advance backwards = %v, want ErrNotNextStage", err)
	}

	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.Stage("shipped")); !errors.Is(err, item.ErrStageUnknown) {
		t.Errorf("Advance to a stage outside the six = %v, want ErrStageUnknown", err)
	}
	if _, err := dispatch.Advance(ctx, dispatchActor, "it_missing", item.StageImplementationPlan); !errors.Is(err, item.ErrNotFound) {
		t.Errorf("Advance on a missing item = %v, want ErrNotFound", err)
	}
}

// TestReturnToCountsNothing is the one way back: the move books nothing,
// because an attempt is counted when a stage is entered to author and what a
// reject does is send the item back to be entered again. The target may be the
// stage the item is at and may not be below it.
func TestReturnToCountsNothing(t *testing.T) {
	ctx, pool, decomposition, dispatch := newWriters(t)
	it := advanceToQueued(ctx, t, decomposition, dispatch)

	before, err := item.Stages(ctx, pool, it.ID)
	if err != nil {
		t.Fatalf("Stages: %v", err)
	}

	sent, err := dispatch.ReturnTo(ctx, dispatchActor, it.ID, item.StageSpec)
	if err != nil {
		t.Fatalf("ReturnTo spec: %v", err)
	}
	if sent.Stage != item.StageSpec {
		t.Errorf("ReturnTo returned stage %s, want spec", sent.Stage)
	}
	after, err := item.Stages(ctx, pool, it.ID)
	if err != nil {
		t.Fatalf("Stages: %v", err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Errorf("the stage rows moved from %+v to %+v, and a return counts nothing", before, after)
	}

	// The stage the item is at is a valid target: a reject at the stage that
	// fired is another attempt at the same artifact.
	if _, err := dispatch.ReturnTo(ctx, dispatchActor, it.ID, item.StageSpec); err != nil {
		t.Fatalf("ReturnTo the stage it is at: %v", err)
	}

	// Forward is Advance's, not this.
	if _, err := dispatch.ReturnTo(ctx, dispatchActor, it.ID, item.StageImplementation); !errors.Is(err, item.ErrNotBackUp) {
		t.Errorf("ReturnTo forwards = %v, want ErrNotBackUp", err)
	}
	for _, stage := range []item.Stage{item.StageQueued, item.StageMerged, item.StageSuperseded, item.Stage("shipped")} {
		if _, err := dispatch.ReturnTo(ctx, dispatchActor, it.ID, stage); !errors.Is(err, item.ErrNotAuthoringStage) {
			t.Errorf("ReturnTo %s = %v, want ErrNotAuthoringStage", stage, err)
		}
	}
	if _, err := dispatch.ReturnTo(ctx, dispatchActor, "it_missing", item.StageSpec); !errors.Is(err, item.ErrNotFound) {
		t.Errorf("ReturnTo on a missing item = %v, want ErrNotFound", err)
	}
	if _, err := dispatch.ReturnTo(ctx, record.Actor{}, it.ID, item.StageSpec); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("ReturnTo with no actor = %v, want ErrKindUnknown", err)
	}
}

// TestEndWritesMergedFromQueuedAlone: merged is the fast-forward's value, and
// queued is the merge queue's membership, so an item that is not in the queue
// was never approved to leave it.
func TestEndWritesMergedFromQueuedAlone(t *testing.T) {
	ctx, pool, decomposition, dispatch := newWriters(t)

	early := oneItem(ctx, t, decomposition)
	if _, err := dispatch.End(ctx, dispatchActor, early.ID); !errors.Is(err, item.ErrNotQueued) {
		t.Errorf("End on an item at spec = %v, want ErrNotQueued", err)
	}

	it := advanceToQueued(ctx, t, decomposition, dispatch)
	merged, err := dispatch.End(ctx, dispatchActor, it.ID)
	if err != nil {
		t.Fatalf("End: %v", err)
	}
	if merged.Stage != item.StageMerged {
		t.Errorf("End returned stage %s, want merged", merged.Stage)
	}
	read, err := item.Get(ctx, pool, it.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Stage != item.StageMerged {
		t.Errorf("the stored stage is %s, want merged", read.Stage)
	}
	if _, err := dispatch.End(ctx, dispatchActor, it.ID); !errors.Is(err, item.ErrNotQueued) {
		t.Errorf("End twice = %v, want ErrNotQueued", err)
	}
}

// TestEscalateAndClearEscalation: the factory saying it cannot do this one, and
// a human taking it over. The attempts already taken stay on the stage, so what
// the clearing writes is the count it cleared at and the limit is compared
// against the attempts since.
func TestEscalateAndClearEscalation(t *testing.T) {
	ctx, pool, decomposition, dispatch := newWriters(t)
	it := oneItem(ctx, t, decomposition)
	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageImplementationPlan); err != nil {
		t.Fatalf("Advance: %v", err)
	}

	escalated, err := dispatch.Escalate(ctx, dispatchActor, it.ID)
	if err != nil {
		t.Fatalf("Escalate: %v", err)
	}
	if escalated.Stage != item.StageEscalated {
		t.Errorf("Escalate returned stage %s, want escalated", escalated.Stage)
	}
	// Nothing escalates from a stage no artifact is authored at.
	if _, err := dispatch.Escalate(ctx, dispatchActor, it.ID); !errors.Is(err, item.ErrNotAuthoringStage) {
		t.Errorf("Escalate an escalated item = %v, want ErrNotAuthoringStage", err)
	}

	cleared, err := dispatch.ClearEscalation(ctx, workActor, it.ID, item.StageImplementationPlan)
	if err != nil {
		t.Fatalf("ClearEscalation: %v", err)
	}
	if cleared.Stage != item.StageImplementationPlan {
		t.Errorf("ClearEscalation returned stage %s, want the stage the human took over", cleared.Stage)
	}
	stages, err := item.Stages(ctx, pool, it.ID)
	if err != nil {
		t.Fatalf("Stages: %v", err)
	}
	var plan item.StageTotals
	for _, s := range stages {
		if s.Stage == item.StageImplementationPlan {
			plan = s
		}
	}
	if plan.Attempts != 1 || plan.ClearedAtAttempts != 1 || plan.AttemptsSinceCleared() != 0 {
		t.Errorf("the plan stage reads %+v, want one attempt cleared at one and none since", plan)
	}

	if _, err := dispatch.ClearEscalation(ctx, workActor, it.ID, item.StageImplementationPlan); !errors.Is(err, item.ErrNotEscalated) {
		t.Errorf("clearing an item that is not escalated = %v, want ErrNotEscalated", err)
	}
	if _, err := dispatch.ClearEscalation(ctx, workActor, it.ID, item.StageQueued); !errors.Is(err, item.ErrNotAuthoringStage) {
		t.Errorf("clearing at queued = %v, want ErrNotAuthoringStage", err)
	}
}

// TestDropEndsAnItemForGood: Work ends one that escalated and nobody took over
// or the intent above it, Ops ends a revert item a mark made unnecessary, and
// an item already ended is out of reach.
func TestDropEndsAnItemForGood(t *testing.T) {
	ctx, pool, decomposition, dispatch := newWriters(t)

	it := oneItem(ctx, t, decomposition)
	dropped, err := dispatch.Drop(ctx, workActor, it.ID)
	if err != nil {
		t.Fatalf("Drop: %v", err)
	}
	if dropped.Stage != item.StageDropped {
		t.Errorf("Drop returned stage %s, want dropped", dropped.Stage)
	}
	read, err := item.Get(ctx, pool, it.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Stage != item.StageDropped {
		t.Errorf("the stored stage is %s, want dropped", read.Stage)
	}
	if _, err := dispatch.Drop(ctx, workActor, it.ID); !errors.Is(err, item.ErrEnded) {
		t.Errorf("dropping a dropped item = %v, want ErrEnded", err)
	}

	merged := advanceToQueued(ctx, t, decomposition, dispatch)
	if _, err := dispatch.End(ctx, dispatchActor, merged.ID); err != nil {
		t.Fatalf("End: %v", err)
	}
	if _, err := dispatch.Drop(ctx, workActor, merged.ID); !errors.Is(err, item.ErrEnded) {
		t.Errorf("dropping a merged item = %v, want ErrEnded", err)
	}
}

// TestPartlyDeliveredIsARepeatableReading: an intent whose items did not all
// ship is at least one stopped item beside at least one live sibling. Nothing
// writes it down, and whether an item is live is the caller's to read.
func TestPartlyDeliveredIsARepeatableReading(t *testing.T) {
	ctx, pool, decomposition, dispatch := newWriters(t)
	const intentID = "in_" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	var made []item.Item
	for _, branch := range []string{"item/first", "item/second"} {
		it, err := decomposition.Create(ctx, decompositionActor, item.New{
			IntentID: intentID, ServiceID: "svc_x", Branch: branch,
		}, "", "", nil)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		made = append(made, it)
	}

	// Both still moving: in progress rather than partly delivered.
	if partly, err := item.PartlyDelivered(ctx, pool, intentID, nil); err != nil || partly {
		t.Errorf("PartlyDelivered with both moving = %v, %v", partly, err)
	}
	// One stopped and none live: stopped rather than partly delivered.
	if _, err := dispatch.Drop(ctx, workActor, made[0].ID); err != nil {
		t.Fatalf("Drop: %v", err)
	}
	if partly, err := item.PartlyDelivered(ctx, pool, intentID, nil); err != nil || partly {
		t.Errorf("PartlyDelivered with nothing live = %v, %v", partly, err)
	}
	// One stopped, one live.
	if partly, err := item.PartlyDelivered(ctx, pool, intentID, []string{made[1].ID}); err != nil || !partly {
		t.Errorf("PartlyDelivered with a live sibling = %v, %v", partly, err)
	}
}

// workActor is who reorders a queue and who ends an item: an owner at Work,
// writing through dispatch rather than beside it.
var workActor = record.Actor{Kind: record.KindHuman, Key: "person:owner", Basis: record.BasisClaimed}

// TestSetPriorityAndAtStage is the settable order: an owner writes a priority
// through dispatch, and the query the merge queue's membership is read with returns
// the items of one service at one stage, greater priority first.
func TestSetPriorityAndAtStage(t *testing.T) {
	ctx, pool, decomposition, dispatch := newWriters(t)
	const serviceID = "svc_" + "00000000000000000000000000000000"

	var queued []item.Item
	for n, branch := range []string{"item/first", "item/second", "item/third"} {
		it, err := decomposition.Create(ctx, decompositionActor, item.New{
			IntentID: fmt.Sprintf("in_%032d", n), ServiceID: serviceID, Branch: branch,
		}, "", "", nil)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		for _, stage := range []item.Stage{item.StageImplementationPlan, item.StageTasks, item.StageImplementation} {
			if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, stage); err != nil {
				t.Fatalf("Advance to %s: %v", stage, err)
			}
		}
		if n < 2 {
			if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageQueued); err != nil {
				t.Fatalf("Advance to queued: %v", err)
			}
			queued = append(queued, it)
		}
	}

	pushed, err := dispatch.SetPriority(ctx, workActor, queued[1].ID, 5)
	if err != nil {
		t.Fatalf("SetPriority: %v", err)
	}
	if pushed.Priority != 5 {
		t.Errorf("SetPriority returned priority %d, want 5", pushed.Priority)
	}

	members, err := item.AtStage(ctx, pool, serviceID, item.StageQueued)
	if err != nil {
		t.Fatalf("AtStage: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("%d items are queued, two were advanced there: %+v", len(members), members)
	}
	if members[0].ID != queued[1].ID {
		t.Errorf("the queued items come back %s then %s, want the pushed one first",
			members[0].ID, members[1].ID)
	}
	if _, err := dispatch.SetPriority(ctx, workActor, "it_missing", 1); !errors.Is(err, item.ErrNotFound) {
		t.Errorf("SetPriority on a missing item = %v, want ErrNotFound", err)
	}
}

// TestNothingAdvancesToAValueThatEndsAnItem: dropped, escalated, and superseded
// each have a write of their own, so none of them is in [item.StageOrder] and
// dispatch refuses an advance to any of them.
func TestNothingAdvancesToAValueThatEndsAnItem(t *testing.T) {
	ctx, _, decomposition, dispatch := newWriters(t)

	it := oneItem(ctx, t, decomposition)
	for _, stage := range []item.Stage{item.StageDropped, item.StageEscalated, item.StageSuperseded} {
		if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, stage); !errors.Is(err, item.ErrStageUnknown) {
			t.Errorf("advancing to %s = %v, want ErrStageUnknown", stage, err)
		}
	}
}

// TestEnterCountsAnotherAttemptAtTheStageTheItemStandsAt: a second attempt at
// one stage is the item entering it again, and the count is what the attempt
// limit is compared against — so it rises on the stored row and outlives the
// process that made the attempt.
func TestEnterCountsAnotherAttemptAtTheStageTheItemStandsAt(t *testing.T) {
	ctx, pool, decomposition, dispatch := newWriters(t)
	it := oneItem(ctx, t, decomposition)

	entered, err := dispatch.Enter(ctx, dispatchActor, it.ID, item.StageSpec)
	if err != nil {
		t.Fatalf("Enter: %v", err)
	}
	if entered.Stage != item.StageSpec {
		t.Errorf("Enter returned stage %s, want the stage the item stands at", entered.Stage)
	}
	stages, err := item.Stages(ctx, pool, it.ID)
	if err != nil {
		t.Fatalf("Stages: %v", err)
	}
	if len(stages) != 1 || stages[0].Attempts != 2 {
		t.Fatalf("Stages returned %+v, want spec at two attempts", stages)
	}
}

// TestEnterRefusesAStageTheItemIsNotAt: entering is another attempt at where
// the item already is; moving it is Advance's or ReturnTo's.
func TestEnterRefusesAStageTheItemIsNotAt(t *testing.T) {
	ctx, _, decomposition, dispatch := newWriters(t)
	it := oneItem(ctx, t, decomposition)

	if _, err := dispatch.Enter(ctx, dispatchActor, it.ID, item.StageImplementation); !errors.Is(err, item.ErrNotAtThatStage) {
		t.Errorf("Enter at a stage the item is not at = %v, want ErrNotAtThatStage", err)
	}
	if _, err := dispatch.Enter(ctx, dispatchActor, it.ID, item.StageQueued); !errors.Is(err, item.ErrNotAuthoringStage) {
		t.Errorf("Enter at queued = %v, want ErrNotAuthoringStage", err)
	}
}
