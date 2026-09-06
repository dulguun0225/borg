package mergequeue_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dulguun0225/borg/factory/halt"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/mergequeue"
)

// TestAHaltStopsEveryCandidateButTheTwoItPasses: while a halt stands the queue
// fast-forwards no candidate, written into the log the way the backlog cap's stop
// is. Nothing is decided, no attempt is counted, and the score learns nothing.
// Two candidates pass it: a revert item and an item the health monitor raised.
func TestAHaltStopsEveryCandidateButTheTwoItPasses(t *testing.T) {
	repo := newRepository()
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{Repository: repo})

	ordinary := queued(ctx, t, pool, token, 1)
	raised := queuedOf(ctx, t, pool, token, refined(ctx, t, pool, token, 2, healthMonitorActor, 0), 2)
	repo.verified[ordinary.ID] = mergequeue.Verified{Commit: "commit-one", BuildID: "bl_one", Passed: true}
	repo.verified[raised.ID] = mergequeue.Verified{Commit: "commit-two", BuildID: "bl_two", Passed: true}

	set, err := halt.NewWriter(pool, token).Insert(ctx, owner, "the owner has lost confidence in the factory")
	if err != nil {
		t.Fatalf("setting the halt: %v", err)
	}

	pass, err := q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(pass.Outcomes) != 2 {
		t.Fatalf("the outcomes are %+v, want one per member", pass.Outcomes)
	}
	stopped, merged := pass.Outcomes[0], pass.Outcomes[1]
	if stopped.ItemID != ordinary.ID || stopped.Stopped != string(mergequeue.WaitHalt) {
		t.Errorf("the ordinary candidate's outcome is %+v, want it held on the halt", stopped)
	}
	if stopped.WaitRow == "" {
		t.Error("the stop names no wait row, and the stop is the row")
	}
	if merged.ItemID != raised.ID || !merged.Merged {
		t.Errorf("the health monitor's item's outcome is %+v, want it merged: the halt passes it", merged)
	}
	if len(repo.reverified) != 1 || repo.reverified[0] != raised.ID {
		t.Errorf("the queue re-verified %v, and a held candidate is not re-verified at all", repo.reverified)
	}
	// Nothing was decided about the held candidate: no attempt was counted, which
	// is what the treatment of a hold means here.
	stages, err := item.Stages(ctx, pool, ordinary.ID)
	if err != nil {
		t.Fatalf("reading the held item's stages: %v", err)
	}
	for _, st := range stages {
		if st.Attempts != 1 {
			t.Errorf("stage %s of the held item stands at %d attempts, want the one its entry counted",
				st.Stage, st.Attempts)
		}
	}

	open, closed := waitsOfKind(t, readLog(t, ctx, pool, token), mergequeue.WaitHalt)
	if len(open) != 1 || len(closed) != 0 {
		t.Fatalf("the log holds %d openings and %d closings of the halt's stop, want one standing", len(open), len(closed))
	}
	var payload mergequeue.WaitPayload
	if err := json.Unmarshal([]byte(open[0].Payload), &payload); err != nil {
		t.Fatalf("reading the wait payload: %v", err)
	}
	if payload.ItemID != ordinary.ID || payload.ServiceID != serviceID {
		t.Errorf("the wait names item %q of service %q", payload.ItemID, payload.ServiceID)
	}

	// The halt is withdrawn, the withdrawal approved at the row that decides one,
	// and the next pass closes the stop and merges the candidate it held.
	withdrawal, err := halt.NewWriter(pool, token).InsertWithdrawal(ctx, owner, set.ID)
	if err != nil {
		t.Fatalf("withdrawing the halt: %v", err)
	}
	if err := halt.NewWriter(pool, token).ApproveWithdrawal(ctx, withdrawal.ID); err != nil {
		t.Fatalf("approving the withdrawal: %v", err)
	}
	pass, err = q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("the second Run: %v", err)
	}
	if len(pass.Outcomes) != 2 {
		t.Fatalf("the second pass's outcomes are %+v", pass.Outcomes)
	}
	if !pass.Outcomes[0].Merged {
		t.Errorf("the candidate the halt held is %+v, want it merged once the halt was withdrawn", pass.Outcomes[0])
	}
	open, closed = waitsOfKind(t, readLog(t, ctx, pool, token), mergequeue.WaitHalt)
	if len(open) != 1 || len(closed) != 1 {
		t.Errorf("the log holds %d openings and %d closings, want the stop ended", len(open), len(closed))
	}
}

// TestTheBacklogCapStopsTheServiceButNotTheRevertsOwnCandidate: how many releases
// may wait behind a rollback hold is the backlog cap, and at the cap the queue
// stops fast-forwarding that service's candidates. The stop does not catch the
// revert's own candidate: the hold lifts only when the revert ships, so a stop
// that held it would never end.
func TestTheBacklogCapStopsTheServiceButNotTheRevertsOwnCandidate(t *testing.T) {
	repo := newRepository()
	behind := &waitingReading{}
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{Repository: repo, Backlog: behind})

	first := queued(ctx, t, pool, token, 1)
	second := queued(ctx, t, pool, token, 2)
	ordinary, revert := first.ID, second.ID
	repo.verified[ordinary] = mergequeue.Verified{Commit: "commit-one", BuildID: "bl_one", Passed: true}
	repo.verified[revert] = mergequeue.Verified{Commit: "commit-two", BuildID: "bl_two", Passed: true}
	behind.reading = mergequeue.Waiting{Standing: true, Releases: 4, Cap: 4, RevertItemID: revert}

	pass, err := q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(pass.Outcomes) != 2 {
		t.Fatalf("the outcomes are %+v, want one per member", pass.Outcomes)
	}
	if pass.Outcomes[0].Stopped != string(mergequeue.WaitBacklogCap) {
		t.Errorf("the ordinary candidate's outcome is %+v, want it held at the cap", pass.Outcomes[0])
	}
	if !pass.Outcomes[1].Merged {
		t.Errorf("the revert's candidate is %+v, want it merged: the stop does not catch it", pass.Outcomes[1])
	}

	open, _ := waitsOfKind(t, readLog(t, ctx, pool, token), mergequeue.WaitBacklogCap)
	if len(open) != 1 {
		t.Fatalf("the log holds %d openings of the cap's stop, want one", len(open))
	}
	var payload mergequeue.WaitPayload
	if err := json.Unmarshal([]byte(open[0].Payload), &payload); err != nil {
		t.Fatalf("reading the wait payload: %v", err)
	}
	if payload.Releases != 4 || payload.Cap != 4 {
		t.Errorf("the wait reports %d releases against a cap of %d", payload.Releases, payload.Cap)
	}

	// Under the cap nothing is stopped, and the pass closes the stop it wrote.
	behind.reading = mergequeue.Waiting{Standing: true, Releases: 2, Cap: 4, RevertItemID: revert}
	if _, err := q.Run(ctx, serviceID); err != nil {
		t.Fatalf("the second Run: %v", err)
	}
	open, closed := waitsOfKind(t, readLog(t, ctx, pool, token), mergequeue.WaitBacklogCap)
	if len(open) != 1 || len(closed) != 1 {
		t.Errorf("the log holds %d openings and %d closings, want the stop ended", len(open), len(closed))
	}
	if len(repo.fastForwards) != 2 {
		t.Errorf("the queue fast-forwarded %v, want the held candidate merged once the pile fell", repo.fastForwards)
	}
}

// TestAHaltPassesARevertWhateverRaisedIt: the halt's first exception is a
// revert, and a revert passes it whatever raised it — the health monitor at a
// failed exit, or a named human at Ops asking for one. The intent of the item
// here is not the health monitor's, so the second exception does not cover it,
// and the reading that says it is a revert is the composition's.
func TestAHaltPassesARevertWhateverRaisedIt(t *testing.T) {
	repo := newRepository()
	reverts := &revertReading{}
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{Repository: repo, Reverts: reverts})

	ordinary := queued(ctx, t, pool, token, 1)
	revert := queued(ctx, t, pool, token, 2)
	repo.verified[ordinary.ID] = mergequeue.Verified{Commit: "commit-one", BuildID: "bl_one", Passed: true}
	repo.verified[revert.ID] = mergequeue.Verified{Commit: "commit-two", BuildID: "bl_two", Passed: true}
	reverts.is = map[string]bool{revert.ID: true}

	if _, err := halt.NewWriter(pool, token).Insert(ctx, owner, "the owner has lost confidence in the factory"); err != nil {
		t.Fatalf("setting the halt: %v", err)
	}

	pass, err := q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(pass.Outcomes) != 2 {
		t.Fatalf("the outcomes are %+v, want one per member", pass.Outcomes)
	}
	stopped, merged := pass.Outcomes[0], pass.Outcomes[1]
	if stopped.ItemID != ordinary.ID || stopped.Stopped != string(mergequeue.WaitHalt) {
		t.Errorf("the ordinary candidate's outcome is %+v, want it held on the halt", stopped)
	}
	if merged.ItemID != revert.ID || !merged.Merged {
		t.Errorf("the revert's outcome is %+v, want it merged: a halt never stops the factory undoing what it did", merged)
	}
	if len(reverts.asked) == 0 {
		t.Error("the queue asked nothing whether an item was a revert, so the exception is not a reading of its own")
	}
}

// TestAFactoryComposedWithNoReaderOfTheRevertsKnowsNoneOfThem: the value a
// factory composed without that reading uses answers for every item, which is
// what [mergequeue.NoRevertKnown] says on its own type.
func TestAFactoryComposedWithNoReaderOfTheRevertsKnowsNoneOfThem(t *testing.T) {
	is, err := mergequeue.NoRevertKnown{}.IsARevert(context.Background(), item.Item{ID: "it_one"})
	if err != nil || is {
		t.Errorf("NoRevertKnown.IsARevert = %v, %v, want false and no error", is, err)
	}
}

// revertReading is the reading of which items are reverts that a test supplies,
// recording what it was asked so that the halt's two exceptions are told apart.
type revertReading struct {
	is    map[string]bool
	asked []string
}

func (r *revertReading) IsARevert(_ context.Context, it item.Item) (bool, error) {
	r.asked = append(r.asked, it.ID)
	return r.is[it.ID], nil
}

// waitingReading is the backlog reading a test moves between passes.
type waitingReading struct {
	reading mergequeue.Waiting
}

func (w *waitingReading) Behind(context.Context, string) (mergequeue.Waiting, error) {
	return w.reading, nil
}

// TestAnIntentThatDoesNotPermitStopsItsItemAndOpensAWait: the queue's membership
// is the items whose intent's state permits it, and an item whose intent does not
// gets a wait row opened by the queue — the component that met the state — and
// closed by it when the state clears.
func TestAnIntentThatDoesNotPermitStopsItsItemAndOpensAWait(t *testing.T) {
	repo := newRepository()
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{Repository: repo})

	in := refined(ctx, t, pool, token, 1, detectorActor, 0)
	it := queuedOf(ctx, t, pool, token, in, 1)
	repo.verified[it.ID] = mergequeue.Verified{Commit: "commit-one", BuildID: "bl_one", Passed: true}

	// The intent goes back to unrefined, which is what reopens the interview.
	if err := intent.NewIntake(pool, token, intent.NoNotifier{}).SendBack(ctx, detectorActor, in.ID, intent.SentBackByReworkRequest); err != nil {
		t.Fatalf("sending the intent back: %v", err)
	}

	members, err := q.Members(ctx, serviceID)
	if err != nil {
		t.Fatalf("Members: %v", err)
	}
	if len(members) != 0 {
		t.Errorf("the members are %v, and an item whose intent does not permit it is not one", ids(members))
	}
	pass, err := q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(pass.Outcomes) != 0 || len(repo.reverified) != 0 {
		t.Errorf("the pass is %+v and re-verified %v, and a stopped item is not reached", pass.Outcomes, repo.reverified)
	}
	open, _ := waitsOfKind(t, readLog(t, ctx, pool, token), mergequeue.WaitIntentStops)
	if len(open) != 1 {
		t.Fatalf("the log holds %d openings of that wait, want one", len(open))
	}
	var payload mergequeue.WaitPayload
	if err := json.Unmarshal([]byte(open[0].Payload), &payload); err != nil {
		t.Fatalf("reading the wait payload: %v", err)
	}
	if payload.ItemID != it.ID || payload.IntentState != string(intent.StateUnrefined) {
		t.Errorf("the wait names item %q in state %q", payload.ItemID, payload.IntentState)
	}

	// The state clears at the round that refines the intent again, and the queue
	// closes the wait and merges the candidate.
	intake := intent.NewIntake(pool, token, intent.NoNotifier{})
	if _, err := intake.Confirm(ctx, detectorActor, intent.Confirmation{
		IntentID: in.ID,
		Requirements: []intent.NewRequirement{
			{Statement: "The system shall do what intent 1 asks, once more"},
		},
	}); err != nil {
		t.Fatalf("refining the intent again: %v", err)
	}
	pass, err = q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("the second Run: %v", err)
	}
	if len(pass.Outcomes) != 1 || !pass.Outcomes[0].Merged {
		t.Fatalf("the second pass's outcomes are %+v, want the candidate merged", pass.Outcomes)
	}
	open, closed := waitsOfKind(t, readLog(t, ctx, pool, token), mergequeue.WaitIntentStops)
	if len(open) != 1 || len(closed) != 1 {
		t.Errorf("the log holds %d openings and %d closings, want the wait ended", len(open), len(closed))
	}
}
