package mergequeue_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/mergequeue"
	"github.com/dulguun0225/borg/factory/release"
)

// TestAHumanAcceptsACommitTheQueueDidNotMake: the wait ends the second way. The
// queue builds the commit, re-verifies it as it re-verifies a candidate, and
// mints its release in master's order naming the build and no item, with the
// closing naming the human as actor.
func TestAHumanAcceptsACommitTheQueueDidNotMake(t *testing.T) {
	repo := newRepository()
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{Repository: repo})

	if _, err := release.NewWriter(pool, token).Mint(ctx, mergequeue.Actor, release.Minting{
		ServiceID: serviceID, BuildID: "bl_one", Commit: "commit-one", ItemID: "it_00000000000000000000000000000001",
	}); err != nil {
		t.Fatalf("minting the release master's head is compared against: %v", err)
	}
	repo.held["commit-one"], repo.held["a-humans-push"] = true, true
	repo.head = "a-humans-push"
	repo.ofCommit["a-humans-push"] = mergequeue.Verified{
		Commit: "a-humans-push", BuildID: "bl_pushed", Passed: true,
	}

	// Nothing to accept before the wait stands.
	if _, err := q.AcceptCommit(ctx, owner, serviceID, "a-humans-push"); !errors.Is(err, mergequeue.ErrNoWaitStanding) {
		t.Errorf("AcceptCommit before the wait = %v, want ErrNoWaitStanding", err)
	}
	if _, err := q.Run(ctx, serviceID); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// A component may not accept one.
	if _, err := q.AcceptCommit(ctx, testActor, serviceID, "a-humans-push"); !errors.Is(err, mergequeue.ErrNotAHuman) {
		t.Errorf("AcceptCommit as a component = %v, want ErrNotAHuman", err)
	}

	accepted, err := q.AcceptCommit(ctx, owner, serviceID, "a-humans-push")
	if err != nil {
		t.Fatalf("AcceptCommit: %v", err)
	}
	if !accepted.Minted() || accepted.Release.Number != 2 {
		t.Fatalf("the acceptance minted %+v, want number two", accepted.Release)
	}
	if accepted.Release.NamesAnItem() {
		t.Error("the release names an item, and a release over an accepted commit names a build and no item")
	}
	if accepted.Release.Commit != "a-humans-push" || accepted.Release.BuildID != "bl_pushed" {
		t.Errorf("the release names commit %q of build %q", accepted.Release.Commit, accepted.Release.BuildID)
	}

	rows := readLog(t, ctx, pool, token)
	open, closed := waitsOfKind(t, rows, mergequeue.WaitMasterHoldsACommitTheQueueDidNotMake)
	if len(open) != 1 || len(closed) != 1 {
		t.Fatalf("the log holds %d openings and %d closings, want the wait ended by the acceptance", len(open), len(closed))
	}
	if closed[0].Actor != owner {
		t.Errorf("the closing was written as %+v, want the human who accepted", closed[0].Actor)
	}
	if err := decisionlog.NewReader(pool, token).Verify(ctx, testReading); err != nil {
		t.Errorf("the chain does not verify: %v", err)
	}
}

// TestAnAcceptedCommitThatFailsItsReverificationLeavesTheWaitStanding: the
// re-verification of an accepted commit runs the contract checks as a candidate's
// does, and a failure leaves the wait standing with the failure on it as a
// rejection row naming the commit and no item — so no attempt is counted, there
// being no item to send anywhere.
func TestAnAcceptedCommitThatFailsItsReverificationLeavesTheWaitStanding(t *testing.T) {
	repo := newRepository()
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{Repository: repo})

	if _, err := release.NewWriter(pool, token).Mint(ctx, mergequeue.Actor, release.Minting{
		ServiceID: serviceID, BuildID: "bl_one", Commit: "commit-one", ItemID: "it_00000000000000000000000000000001",
	}); err != nil {
		t.Fatalf("minting the release master's head is compared against: %v", err)
	}
	repo.held["commit-one"], repo.held["a-humans-push"] = true, true
	repo.head = "a-humans-push"
	repo.ofCommit["a-humans-push"] = mergequeue.Verified{
		Commit: "a-humans-push", BuildID: "bl_pushed",
		Why: "the producer's own contract diff is breaking and the migration has not shipped",
	}
	if _, err := q.Run(ctx, serviceID); err != nil {
		t.Fatalf("Run: %v", err)
	}

	accepted, err := q.AcceptCommit(ctx, owner, serviceID, "a-humans-push")
	if err != nil {
		t.Fatalf("AcceptCommit: %v", err)
	}
	if accepted.Minted() {
		t.Errorf("the acceptance minted %+v, and a re-verification that fails mints nothing", accepted.Release)
	}
	if accepted.Why == "" || accepted.RejectionRow == "" {
		t.Errorf("the acceptance reports why %q and row %q", accepted.Why, accepted.RejectionRow)
	}

	rows := readLog(t, ctx, pool, token)
	open, closed := waitsOfKind(t, rows, mergequeue.WaitMasterHoldsACommitTheQueueDidNotMake)
	if len(open) != 1 || len(closed) != 0 {
		t.Errorf("the log holds %d openings and %d closings, want the wait still standing", len(open), len(closed))
	}
	var payload mergequeue.RejectionPayload
	for _, row := range rows {
		if row.Shape == decisionlog.ShapeQueueRejection {
			if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
				t.Fatalf("reading the rejection payload: %v", err)
			}
		}
	}
	if payload.Commit != "a-humans-push" || payload.ItemID != "" {
		t.Errorf("the rejection names commit %q and item %q, want the commit and no item", payload.Commit, payload.ItemID)
	}
	if payload.CountsAnAttempt || payload.ReturnsTo != "" {
		t.Errorf("the rejection counts an attempt %v and returns to %q, and there is no item to send anywhere",
			payload.CountsAnAttempt, payload.ReturnsTo)
	}
}

// TestAcceptanceReadsMasterBeforeItMints: what an acceptance reads is master
// against the records, the same way every other mint reads it, before it
// mints. A build that explains the commit may appear after the wait was
// opened and before the human accepts it — the commit is then the queue's own
// unfinished merge and not a foreign push, and the acceptance completes it the
// way the queue always completes one rather than minting a release naming no
// item.
func TestAcceptanceReadsMasterBeforeItMints(t *testing.T) {
	repo := newRepository()
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{Repository: repo})

	if _, err := release.NewWriter(pool, token).Mint(ctx, mergequeue.Actor, release.Minting{
		ServiceID: serviceID, BuildID: "bl_one", Commit: "commit-one", ItemID: "it_00000000000000000000000000000001",
	}); err != nil {
		t.Fatalf("minting the release master's head is compared against: %v", err)
	}
	it := queued(ctx, t, pool, token, 1)
	repo.held["commit-one"], repo.held["a-humans-push"] = true, true
	repo.head = "a-humans-push"

	// At this pass no build explains the head, so the wait opens.
	if _, err := q.Run(ctx, serviceID); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Only afterwards does the item's own build appear at the commit master
	// already holds.
	made := built(ctx, t, pool, token, it, "a-humans-push", "", nil)
	approveAtMergeToMaster(ctx, t, pool, token, it.ID)
	repo.verified[it.ID] = mergequeue.Verified{Commit: "a-humans-push", BuildID: made.ID, Passed: true}

	accepted, err := q.AcceptCommit(ctx, owner, serviceID, "a-humans-push")
	if err != nil {
		t.Fatalf("AcceptCommit: %v", err)
	}
	if !accepted.Minted() || !accepted.Release.NamesAnItem() || accepted.Release.ItemID != it.ID {
		t.Fatalf("the acceptance minted %+v, want it completed as %s's own unfinished merge", accepted.Release, it.ID)
	}
	if accepted.Release.Number != 2 {
		t.Errorf("the release is numbered %d, want two", accepted.Release.Number)
	}
	open, closed := waitsOfKind(t, readLog(t, ctx, pool, token), mergequeue.WaitMasterHoldsACommitTheQueueDidNotMake)
	if len(open) != 1 || len(closed) != 1 {
		t.Errorf("the log holds %d openings and %d closings, want the wait ended", len(open), len(closed))
	}
}
