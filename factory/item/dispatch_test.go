// This file holds the tests of Dispatch's writes: advancing a stage, sending
// one back for rework, reporting an attempt, and setting priority.
package item_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
)

func TestAdvanceMovesOneStageForward(t *testing.T) {
	ctx, pool, decomposition, dispatch := newWriters(t)
	it := oneItem(ctx, t, decomposition)

	advanced, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageImplementation)
	if err != nil {
		t.Fatalf("Advance to implementation: %v", err)
	}
	if advanced.Stage != item.StageImplementation {
		t.Errorf("Advance returned stage %s, want implementation", advanced.Stage)
	}
	// The advance rewrites the stage and nothing else.
	advanced.Stage = it.Stage
	if !reflect.DeepEqual(advanced, it) {
		t.Errorf("Advance rewrote more than the stage: %+v, decomposed as %+v", advanced, it)
	}

	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageQueued); err != nil {
		t.Fatalf("Advance to queued: %v", err)
	}
	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageMerged); err != nil {
		t.Fatalf("Advance to merged: %v", err)
	}
	read, err := item.Get(ctx, pool, it.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Stage != item.StageMerged {
		t.Errorf("the item is at %s, want merged", read.Stage)
	}

	// Merged is the last stage anything writes; nothing advances past it.
	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageImplementation); !errors.Is(err, item.ErrNotNextStage) {
		t.Errorf("Advance past merged = %v, want ErrNotNextStage", err)
	}
}

func TestAdvanceRefusesSkipsAndBackwardsMoves(t *testing.T) {
	ctx, _, decomposition, dispatch := newWriters(t)
	it := oneItem(ctx, t, decomposition)

	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageQueued); !errors.Is(err, item.ErrNotNextStage) {
		t.Errorf("Advance skipping implementation = %v, want ErrNotNextStage", err)
	}
	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageSpec); !errors.Is(err, item.ErrNotNextStage) {
		t.Errorf("Advance to the stage it is at = %v, want ErrNotNextStage", err)
	}

	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageImplementation); err != nil {
		t.Fatalf("Advance to implementation: %v", err)
	}
	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageSpec); !errors.Is(err, item.ErrNotNextStage) {
		t.Errorf("Advance backwards = %v, want ErrNotNextStage", err)
	}

	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.Stage("shipped")); !errors.Is(err, item.ErrStageUnknown) {
		t.Errorf("Advance to a stage outside the four = %v, want ErrStageUnknown", err)
	}
	if _, err := dispatch.Advance(ctx, dispatchActor, "it_missing", item.StageImplementation); !errors.Is(err, item.ErrNotFound) {
		t.Errorf("Advance on a missing item = %v, want ErrNotFound", err)
	}
}

func TestReportAttemptAccumulatesPerStage(t *testing.T) {
	ctx, pool, decomposition, dispatch := newWriters(t)
	it := oneItem(ctx, t, decomposition)

	if err := dispatch.ReportAttempt(ctx, dispatchActor, it.ID, item.StageSpec, 100); err != nil {
		t.Fatalf("ReportAttempt: %v", err)
	}
	if err := dispatch.ReportAttempt(ctx, dispatchActor, it.ID, item.StageSpec, 50); err != nil {
		t.Fatalf("ReportAttempt again: %v", err)
	}
	if err := dispatch.ReportAttempt(ctx, dispatchActor, it.ID, item.StageImplementation, 10); err != nil {
		t.Fatalf("ReportAttempt at implementation: %v", err)
	}

	stages, err := item.Stages(ctx, pool, it.ID)
	if err != nil {
		t.Fatalf("Stages: %v", err)
	}
	if len(stages) != 2 {
		t.Fatalf("Stages returned %d rows, want 2", len(stages))
	}
	spec, implementation := stages[0], stages[1]
	if spec.Stage != item.StageSpec || implementation.Stage != item.StageImplementation {
		t.Fatalf("Stages = %+v, want spec then implementation, in first-report order", stages)
	}
	if spec.Attempts != 2 || spec.SpendTokens != 150 {
		t.Errorf("spec totals %d attempts and %d tokens, want 2 and 150", spec.Attempts, spec.SpendTokens)
	}
	if implementation.Attempts != 1 || implementation.SpendTokens != 10 {
		t.Errorf("implementation totals %d attempts and %d tokens, want 1 and 10", implementation.Attempts, implementation.SpendTokens)
	}

	if err := dispatch.ReportAttempt(ctx, dispatchActor, it.ID, item.StageSpec, -1); !errors.Is(err, item.ErrSpendNegative) {
		t.Errorf("ReportAttempt with negative spend = %v, want ErrSpendNegative", err)
	}
	if err := dispatch.ReportAttempt(ctx, dispatchActor, it.ID, item.Stage("shipped"), 1); !errors.Is(err, item.ErrStageUnknown) {
		t.Errorf("ReportAttempt at a stage outside the three = %v, want ErrStageUnknown", err)
	}
}

// TestReworkRequestMovesUpAndCountsTheAttempt is the one way back: the rework is booked
// against the thing that was wrong, so the move and the attempt are one write. The
// target may be the stage the item is at — a reject at the stage that fired is
// another attempt at the same artifact — and may not be below it.
func TestReworkRequestMovesUpAndCountsTheAttempt(t *testing.T) {
	ctx, pool, decomposition, dispatch := newWriters(t)
	it := oneItem(ctx, t, decomposition)
	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageImplementation); err != nil {
		t.Fatalf("Advance to implementation: %v", err)
	}
	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageQueued); err != nil {
		t.Fatalf("Advance to queued: %v", err)
	}

	sent, err := dispatch.ReworkRequest(ctx, dispatchActor, it.ID, item.StageImplementation)
	if err != nil {
		t.Fatalf("ReworkRequest to implementation: %v", err)
	}
	if sent.Stage != item.StageImplementation {
		t.Errorf("ReworkRequest returned stage %s, want implementation", sent.Stage)
	}
	stages, err := item.Stages(ctx, pool, it.ID)
	if err != nil {
		t.Fatalf("Stages: %v", err)
	}
	if len(stages) != 1 || stages[0].Stage != item.StageImplementation || stages[0].Attempts != 1 {
		t.Fatalf("the stage rows are %+v, want one attempt booked at implementation", stages)
	}
	if stages[0].SpendTokens != 0 {
		t.Errorf("the rework request spent %d tokens, and what the attempt after it spends is that attempt's",
			stages[0].SpendTokens)
	}

	// The stage the item is at is a valid target and counts another attempt.
	if _, err := dispatch.ReworkRequest(ctx, dispatchActor, it.ID, item.StageImplementation); err != nil {
		t.Fatalf("ReworkRequest to the stage it is at: %v", err)
	}
	if stages, err = item.Stages(ctx, pool, it.ID); err != nil || stages[0].Attempts != 2 {
		t.Fatalf("the implementation stage records %d attempts, want 2: %v", stages[0].Attempts, err)
	}

	// Forward is Advance's, not this.
	if _, err := dispatch.ReworkRequest(ctx, dispatchActor, it.ID, item.StageQueued); !errors.Is(err, item.ErrNotBackUp) {
		t.Errorf("ReworkRequest forwards = %v, want ErrNotBackUp", err)
	}
	if _, err := dispatch.ReworkRequest(ctx, dispatchActor, it.ID, item.Stage("shipped")); !errors.Is(err, item.ErrStageUnknown) {
		t.Errorf("ReworkRequest to a stage outside the four = %v, want ErrStageUnknown", err)
	}
	if _, err := dispatch.ReworkRequest(ctx, dispatchActor, "it_missing", item.StageSpec); !errors.Is(err, item.ErrNotFound) {
		t.Errorf("ReworkRequest on a missing item = %v, want ErrNotFound", err)
	}
	if _, err := dispatch.ReworkRequest(ctx, record.Actor{}, it.ID, item.StageSpec); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("ReworkRequest with no actor = %v, want ErrKindUnknown", err)
	}
}

// workActor is who reorders a queue: an owner at Work, writing the priority
// through dispatch rather than beside it.
var workActor = record.Actor{Kind: record.KindHuman, Name: "owner"}

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
		})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageImplementation); err != nil {
			t.Fatalf("Advance: %v", err)
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

// TestNothingAdvancesToOrIsSentBackToSuperseded: it is a terminal value and is not in
// [item.StageOrder], so dispatch refuses it in both directions.
func TestNothingAdvancesToOrIsSentBackToSuperseded(t *testing.T) {
	ctx, _, decomposition, dispatch := newWriters(t)

	it := oneItem(ctx, t, decomposition)
	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageSuperseded); !errors.Is(err, item.ErrStageUnknown) {
		t.Errorf("advancing to superseded = %v, want ErrStageUnknown", err)
	}
	if _, err := dispatch.ReworkRequest(ctx, dispatchActor, it.ID, item.StageSuperseded); !errors.Is(err, item.ErrStageUnknown) {
		t.Errorf("sending back to superseded = %v, want ErrStageUnknown", err)
	}
}
