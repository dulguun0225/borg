package mergequeue_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/mergequeue"
	"github.com/dulguun0225/borg/factory/release"
)

// TestACommitTheQueueDidNotMakeHoldsTheService: master's head is past the newest
// release's commit and no build of a candidate approved at Merge to master names
// it, so the queue mints nothing for the service and writes the wait, naming the
// service and the commit.
func TestACommitTheQueueDidNotMakeHoldsTheService(t *testing.T) {
	repo := newRepository()
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{Repository: repo})

	it := queued(ctx, t, pool, token, 1)
	repo.verified[it.ID] = mergequeue.Verified{Commit: "commit-two", BuildID: "bl_two", Passed: true}

	// A release stands, master holds its commit, and somebody pushed a commit on
	// top of it that no build names.
	if _, err := release.NewWriter(pool, token).Mint(ctx, mergequeue.Actor, release.Minting{
		ServiceID: serviceID, BuildID: "bl_one", Commit: "commit-one", ItemID: "it_00000000000000000000000000000001",
	}); err != nil {
		t.Fatalf("minting the release master's head is compared against: %v", err)
	}
	repo.held["commit-one"], repo.held["a-humans-push"] = true, true
	repo.head = "a-humans-push"

	pass, err := q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if pass.Stopped != string(mergequeue.WaitMasterHoldsACommitTheQueueDidNotMake) {
		t.Fatalf("the pass reports %q, want the service held on a commit the queue did not make", pass.Stopped)
	}
	if pass.StopWaitRow == "" {
		t.Error("the stop names no wait row, and the stop is the row")
	}
	if len(repo.reverified) != 0 || len(repo.fastForwards) != 0 {
		t.Errorf("the queue re-verified %v and fast-forwarded %v, and a held service mints nothing",
			repo.reverified, repo.fastForwards)
	}
	var releases int
	if err := pool.QueryRow(ctx, `select count(*) from `+release.Table).Scan(&releases); err != nil {
		t.Fatalf("counting releases: %v", err)
	}
	if releases != 1 {
		t.Errorf("%d releases exist, and the queue minted nothing for the held service", releases)
	}

	rows := readLog(t, ctx, pool, token)
	open, closed := waitsOfKind(t, rows, mergequeue.WaitMasterHoldsACommitTheQueueDidNotMake)
	if len(open) != 1 || len(closed) != 0 {
		t.Fatalf("the log holds %d openings and %d closings of that wait, want one standing", len(open), len(closed))
	}
	var payload mergequeue.WaitPayload
	if err := json.Unmarshal([]byte(open[0].Payload), &payload); err != nil {
		t.Fatalf("reading the wait payload: %v", err)
	}
	if payload.ServiceID != serviceID || payload.Commit != "a-humans-push" {
		t.Errorf("the wait names service %q and commit %q", payload.ServiceID, payload.Commit)
	}
	if open[0].Actor != mergequeue.Actor {
		t.Errorf("the wait was written as %+v, want the queue", open[0].Actor)
	}

	// A second pass over the same condition writes no second row: the condition is
	// met at every pass while it holds.
	if _, err := q.Run(ctx, serviceID); err != nil {
		t.Fatalf("the second Run: %v", err)
	}
	open, _ = waitsOfKind(t, readLog(t, ctx, pool, token), mergequeue.WaitMasterHoldsACommitTheQueueDidNotMake)
	if len(open) != 1 {
		t.Errorf("the log holds %d openings of that wait after two passes, want one", len(open))
	}

	// Master returned to the commit the newest release names, which is repository
	// administration: the queue reads it at its next pass, closes the wait, and
	// merges again.
	repo.head = "commit-one"
	pass, err = q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("the third Run: %v", err)
	}
	if pass.Stopped != "" {
		t.Errorf("the pass still reports %q after master returned", pass.Stopped)
	}
	open, closed = waitsOfKind(t, readLog(t, ctx, pool, token), mergequeue.WaitMasterHoldsACommitTheQueueDidNotMake)
	if len(open) != 1 || len(closed) != 1 {
		t.Errorf("the log holds %d openings and %d closings, want the one wait ended", len(open), len(closed))
	}
	if len(repo.fastForwards) != 1 {
		t.Errorf("the queue fast-forwarded %v, want the candidate merged once the wait ended", repo.fastForwards)
	}
}

// TestAReleaseNamingACommitMasterDoesNotHoldHoldsTheService: git was restored
// behind the graph, so the records name a commit master does not hold. The queue
// mints nothing and the wait ends only when master holds the commit again,
// because a release is written once and cannot be unwritten to match.
func TestAReleaseNamingACommitMasterDoesNotHoldHoldsTheService(t *testing.T) {
	repo := newRepository()
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{Repository: repo})

	it := queued(ctx, t, pool, token, 1)
	repo.verified[it.ID] = mergequeue.Verified{Commit: "commit-two", BuildID: "bl_two", Passed: true}
	minted, err := release.NewWriter(pool, token).Mint(ctx, mergequeue.Actor, release.Minting{
		ServiceID: serviceID, BuildID: "bl_one", Commit: "commit-one", ItemID: "it_00000000000000000000000000000001",
	})
	if err != nil {
		t.Fatalf("minting the release the restore left behind: %v", err)
	}
	// Master holds an earlier commit and not the one the release names.
	repo.head, repo.held["an-older-commit"] = "an-older-commit", true

	pass, err := q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if pass.Stopped != string(mergequeue.WaitAReleaseNamesACommitMasterDoesNotHold) {
		t.Fatalf("the pass reports %q, want the service held on a release master does not hold", pass.Stopped)
	}
	open, _ := waitsOfKind(t, readLog(t, ctx, pool, token), mergequeue.WaitAReleaseNamesACommitMasterDoesNotHold)
	if len(open) != 1 {
		t.Fatalf("the log holds %d openings of that wait, want one", len(open))
	}
	var payload mergequeue.WaitPayload
	if err := json.Unmarshal([]byte(open[0].Payload), &payload); err != nil {
		t.Fatalf("reading the wait payload: %v", err)
	}
	if payload.ReleaseID != minted.ID || payload.Commit != "commit-one" {
		t.Errorf("the wait names release %q and commit %q, want %s and commit-one", payload.ReleaseID, payload.Commit, minted.ID)
	}
	if len(repo.reverified) != 0 {
		t.Errorf("the queue re-verified %v, and a service held at this reading mints nothing", repo.reverified)
	}

	// Master holds the commit again, from a clone that had it.
	repo.head, repo.held["commit-one"] = "commit-one", true
	if _, err := q.Run(ctx, serviceID); err != nil {
		t.Fatalf("the second Run: %v", err)
	}
	open, closed := waitsOfKind(t, readLog(t, ctx, pool, token), mergequeue.WaitAReleaseNamesACommitMasterDoesNotHold)
	if len(open) != 1 || len(closed) != 1 {
		t.Errorf("the log holds %d openings and %d closings, want the one wait ended", len(open), len(closed))
	}
}

// TestTheQueueCompletesItsOwnUnfinishedMerge: master's head is past the newest
// release's and a build of a candidate approved at Merge to master names it, so
// it is the queue's own unfinished merge — a fast-forward that landed and a
// record write that did not. The queue completes it in master's order with the
// write the fast-forward already implied, and fast-forwards nothing to do it.
func TestTheQueueCompletesItsOwnUnfinishedMerge(t *testing.T) {
	repo := newRepository()
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{Repository: repo})

	if _, err := release.NewWriter(pool, token).Mint(ctx, mergequeue.Actor, release.Minting{
		ServiceID: serviceID, BuildID: "bl_one", Commit: "commit-one", ItemID: "it_00000000000000000000000000000001",
	}); err != nil {
		t.Fatalf("minting the release before the unfinished merge: %v", err)
	}
	it := queued(ctx, t, pool, token, 1)
	made := built(ctx, t, pool, token, it, "commit-two", "", nil)
	repo.verified[it.ID] = mergequeue.Verified{Commit: "commit-two", BuildID: made.ID, Passed: true}
	repo.held["commit-one"], repo.held["commit-two"] = true, true
	repo.head = "commit-two"

	pass, err := q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if pass.Stopped != "" {
		t.Fatalf("the pass reports %q, and the queue's own unfinished merge is completed rather than held", pass.Stopped)
	}
	if pass.Master.CompletedItemID != it.ID {
		t.Errorf("the reading completed %q, want the unfinished merge of %s", pass.Master.CompletedItemID, it.ID)
	}
	if len(pass.Outcomes) != 1 || !pass.Outcomes[0].Merged || pass.Outcomes[0].Release.Number != 2 {
		t.Fatalf("the outcomes are %+v, want one merge minted as number two", pass.Outcomes)
	}
	if len(repo.fastForwards) != 0 {
		t.Errorf("the queue fast-forwarded %v, and master already holds the commit", repo.fastForwards)
	}
	// The candidate is not re-verified a second time as a member: the release it
	// now has is what says its fast-forward happened.
	if len(repo.reverified) != 1 {
		t.Errorf("the queue re-verified %v, want the one read that produced the forms", repo.reverified)
	}
}

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

// TestAServiceWithNoReleaseIsComparedAgainstNothing: what master holds before the
// first merge is whatever created the repository, and no record says otherwise, so
// the reading holds nothing.
func TestAServiceWithNoReleaseIsComparedAgainstNothing(t *testing.T) {
	repo := newRepository()
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{Repository: repo})
	it := queued(ctx, t, pool, token, 1)
	repo.head, repo.held["the-initial-commit"] = "the-initial-commit", true
	repo.verified[it.ID] = mergequeue.Verified{Commit: "commit-one", BuildID: "bl_one", Passed: true}

	pass, err := q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if pass.Stopped != "" || len(pass.Outcomes) != 1 || !pass.Outcomes[0].Merged {
		t.Fatalf("the pass is %+v, want the first candidate merged", pass)
	}
}

// TestMasterIsReadBeforeEveryMintAndNotOnlyAtTheStart: a commit that arrives on
// master while a pass is running is a commit the queue did not make, so the pass
// stops there rather than merging the candidates behind it onto a master nothing
// read.
func TestMasterIsReadBeforeEveryMintAndNotOnlyAtTheStart(t *testing.T) {
	repo := newRepository()
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{Repository: repo})

	first := queued(ctx, t, pool, token, 1)
	second := queued(ctx, t, pool, token, 2)
	repo.verified[first.ID] = mergequeue.Verified{Commit: "commit-one", BuildID: "bl_one", Passed: true}
	repo.verified[second.ID] = mergequeue.Verified{Commit: "commit-two", BuildID: "bl_two", Passed: true}
	repo.onFastForward = func(commit string) {
		if commit == "commit-one" {
			repo.head, repo.held["a-humans-push"] = "a-humans-push", true
		}
	}

	pass, err := q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if pass.Stopped != string(mergequeue.WaitMasterHoldsACommitTheQueueDidNotMake) {
		t.Fatalf("the pass reports %q, want the service held on the commit that arrived mid-pass", pass.Stopped)
	}
	if len(pass.Outcomes) != 1 || !pass.Outcomes[0].Merged {
		t.Fatalf("the outcomes are %+v, want the first merged and the second not reached", pass.Outcomes)
	}
	if len(repo.fastForwards) != 1 {
		t.Errorf("the queue fast-forwarded %v, want the one merge it made before the reading held it", repo.fastForwards)
	}
	open, _ := waitsOfKind(t, readLog(t, ctx, pool, token), mergequeue.WaitMasterHoldsACommitTheQueueDidNotMake)
	if len(open) != 1 {
		t.Errorf("the log holds %d openings of that wait, want one", len(open))
	}
}

// TestBeforeMintRecognisesACandidatesBuildTheSameWayTheStartDoes: the reading
// before every mint is the same reading the start makes, branch for branch. A
// commit that arrives on master mid-pass and names a later candidate's own
// approved build is that candidate's own unfinished merge, completed in
// master's order, and not a commit the queue did not make.
func TestBeforeMintRecognisesACandidatesBuildTheSameWayTheStartDoes(t *testing.T) {
	repo := newRepository()
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{Repository: repo})

	first := queued(ctx, t, pool, token, 1)
	second := queued(ctx, t, pool, token, 2)
	repo.verified[first.ID] = mergequeue.Verified{Commit: "commit-one", BuildID: "bl_one", Passed: true}
	madeSecond := built(ctx, t, pool, token, second, "commit-two", "", nil)
	repo.verified[second.ID] = mergequeue.Verified{Commit: "commit-two", BuildID: madeSecond.ID, Passed: true}
	// Something outside this pass fast-forwards master straight past the first
	// candidate's own merge, to the second candidate's already-approved commit.
	repo.onFastForward = func(commit string) {
		if commit == "commit-one" {
			repo.head, repo.held["commit-two"] = "commit-two", true
		}
	}

	pass, err := q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if pass.Stopped != "" {
		t.Fatalf("the pass reports %q, want the second candidate's own build recognised rather than a stop", pass.Stopped)
	}
	if len(pass.Outcomes) != 2 || !pass.Outcomes[0].Merged || !pass.Outcomes[1].Merged {
		t.Fatalf("the outcomes are %+v, want both candidates merged", pass.Outcomes)
	}
	if pass.Outcomes[1].ItemID != second.ID || pass.Outcomes[1].Release.Number != 2 {
		t.Errorf("the second outcome is %+v, want %s minted as number two", pass.Outcomes[1], second.ID)
	}
	// The second candidate's own fast-forward never runs: the commit is already
	// on master, and the write its fast-forward already implied is what completes
	// it.
	if len(repo.fastForwards) != 1 || repo.fastForwards[0] != "commit-one" {
		t.Errorf("the queue fast-forwarded %v, want only the first candidate's own merge", repo.fastForwards)
	}
}

// TestRestartReadsMasterAndFastForwardsNothing is the queue's restart: it makes
// the same reading a run makes before it mints — so a service master and the
// records disagree over is reported held at the start rather than at the first
// mint — and it merges nothing, a start being a read of the queue's own records
// and not a pass of the queue.
func TestRestartReadsMasterAndFastForwardsNothing(t *testing.T) {
	repo := newRepository()
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{Repository: repo})

	it := queued(ctx, t, pool, token, 1)
	repo.verified[it.ID] = mergequeue.Verified{Commit: "commit-two", BuildID: "bl_two", Passed: true}
	if _, err := release.NewWriter(pool, token).Mint(ctx, mergequeue.Actor, release.Minting{
		ServiceID: serviceID, BuildID: "bl_one", Commit: "commit-one", ItemID: "it_00000000000000000000000000000001",
	}); err != nil {
		t.Fatalf("minting the release master's head is compared against: %v", err)
	}
	repo.held["commit-one"], repo.held["a-humans-push"] = true, true
	repo.head = "a-humans-push"

	read, completed, err := q.Restart(ctx, serviceID)
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if read.Stopped != string(mergequeue.WaitMasterHoldsACommitTheQueueDidNotMake) {
		t.Fatalf("the restart reports %q, want the service held on a commit the queue did not make", read.Stopped)
	}
	if read.WaitRow == "" {
		t.Error("the stop names no wait row, and the stop is the row")
	}
	if len(completed) != 0 {
		t.Errorf("the restart wrote %d outcome(s), and no merge of its own was unfinished", len(completed))
	}
	if len(repo.reverified) != 0 || len(repo.fastForwards) != 0 {
		t.Errorf("the restart re-verified %v and fast-forwarded %v, and a start merges nothing",
			repo.reverified, repo.fastForwards)
	}
	if _, _, err := q.Restart(ctx, ""); !errors.Is(err, mergequeue.ErrServiceIDEmpty) {
		t.Errorf("Restart with no service = %v, want ErrServiceIDEmpty", err)
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

// TestMasterIsComparedAgainstEveryApprovedBuildNotJustQueuedItems: the design
// compares master's head against the builds the records hold, whatever the
// item's stage now — an item the queue itself sent back after its own approval
// still explains a commit its build names. Only an item still at [StageQueued]
// used to be looked at, which missed this one.
func TestMasterIsComparedAgainstEveryApprovedBuildNotJustQueuedItems(t *testing.T) {
	repo := newRepository()
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{Repository: repo})

	if _, err := release.NewWriter(pool, token).Mint(ctx, mergequeue.Actor, release.Minting{
		ServiceID: serviceID, BuildID: "bl_one", Commit: "commit-one", ItemID: "it_00000000000000000000000000000001",
	}); err != nil {
		t.Fatalf("minting the release before the unfinished merge: %v", err)
	}
	it := queued(ctx, t, pool, token, 1)
	made := built(ctx, t, pool, token, it, "commit-two", "", nil)
	repo.verified[it.ID] = mergequeue.Verified{Commit: "commit-two", BuildID: made.ID, Passed: true}
	repo.held["commit-one"], repo.held["commit-two"] = true, true
	repo.head = "commit-two"

	// The item was sent back to Implementation — by the queue itself, on some
	// earlier attempt at another commit — after the Merge to master gate had
	// already approved it once. The stage the item stands at now is not queued.
	if _, err := item.NewDispatch(pool, token).ReturnTo(ctx, dispatchActor, it.ID, item.StageImplementation); err != nil {
		t.Fatalf("sending the item back: %v", err)
	}

	pass, err := q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if pass.Stopped != "" {
		t.Fatalf("the pass reports %q, want the already-approved build recognised rather than a stop", pass.Stopped)
	}
	if pass.Master.CompletedItemID != it.ID {
		t.Errorf("the reading completed %q, want the unfinished merge of %s", pass.Master.CompletedItemID, it.ID)
	}
	if len(pass.Outcomes) != 1 || !pass.Outcomes[0].Merged || pass.Outcomes[0].Release.Number != 2 {
		t.Fatalf("the outcomes are %+v, want one merge minted as number two", pass.Outcomes)
	}
}

// TestAnUnfinishedMergeTheIntentStopsStandsAsAWait: an item the intent's state
// stops is not minted for even where its own approved build already explains
// the commit master holds. Its unfinished merge stands as a wait, the way the
// halt's does, rather than being completed for an item nothing may move.
func TestAnUnfinishedMergeTheIntentStopsStandsAsAWait(t *testing.T) {
	repo := newRepository()
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{Repository: repo})

	in := refined(ctx, t, pool, token, 1, detectorActor, 0)
	it := queuedOf(ctx, t, pool, token, in, 1)
	made := built(ctx, t, pool, token, it, "commit-two", "", nil)
	repo.verified[it.ID] = mergequeue.Verified{Commit: "commit-two", BuildID: made.ID, Passed: true}
	if _, err := release.NewWriter(pool, token).Mint(ctx, mergequeue.Actor, release.Minting{
		ServiceID: serviceID, BuildID: "bl_one", Commit: "commit-one", ItemID: "it_00000000000000000000000000000001",
	}); err != nil {
		t.Fatalf("minting the release before the unfinished merge: %v", err)
	}
	repo.held["commit-one"], repo.held["commit-two"] = true, true
	repo.head = "commit-two"

	// The intent goes back to unrefined, which stops the item.
	if err := intent.NewIntake(pool, token, intent.NoNotifier{}).SendBack(ctx, detectorActor, in.ID, intent.SentBackByReworkRequest); err != nil {
		t.Fatalf("sending the intent back: %v", err)
	}

	pass, err := q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(pass.Outcomes) != 0 {
		t.Fatalf("the outcomes are %+v, and an item the intent stops is not minted for", pass.Outcomes)
	}
	if pass.Stopped != string(mergequeue.WaitIntentStops) {
		t.Fatalf("the pass reports %q, want the unfinished merge standing as a wait the intent's state opens", pass.Stopped)
	}
	if len(repo.reverified) != 0 {
		t.Errorf("the queue re-verified %v, and an item the intent stops decides nothing", repo.reverified)
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

	// The state clears, and the next pass completes what was standing.
	if _, err := intent.NewIntake(pool, token, intent.NoNotifier{}).Confirm(ctx, detectorActor, intent.Confirmation{
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
	if pass.Master.CompletedItemID != it.ID || len(pass.Outcomes) != 1 || !pass.Outcomes[0].Merged {
		t.Fatalf("the second pass is %+v, want the unfinished merge completed", pass)
	}
}
