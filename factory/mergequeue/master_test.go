package mergequeue_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
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
	if err := decisionlog.NewReader(pool, token).Verify(ctx, testActor); err != nil {
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

// TestTheMintTakesTheHigherOfTwoReadingsAndWritesTheNumbersItSkipped: the numbers
// a restore lost are above the highest record, and what says how high they went
// is the health monitor's store. The mint seats the release above the higher of
// the two readings and writes the numbers it passed over into the log.
func TestTheMintTakesTheHigherOfTwoReadingsAndWritesTheNumbersItSkipped(t *testing.T) {
	repo := newRepository()
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{
		Repository: repo,
		Numbers:    seen{serviceID: 7},
	})

	it := queued(ctx, t, pool, token, 1)
	repo.verified[it.ID] = mergequeue.Verified{Commit: "commit-eight", BuildID: "bl_eight", Passed: true}

	pass, err := q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(pass.Outcomes) != 1 || pass.Outcomes[0].Release.Number != 8 {
		t.Fatalf("the outcomes are %+v, want the release seated at number eight", pass.Outcomes)
	}
	skipped := pass.Outcomes[0].SkippedNumbers
	if len(skipped) != 7 || skipped[0] != 1 || skipped[6] != 7 {
		t.Errorf("the mint reports %v skipped, want one through seven", skipped)
	}

	var payload mergequeue.SkippedNumbersPayload
	found := false
	for _, row := range readLog(t, ctx, pool, token) {
		if row.Shape != decisionlog.ShapeInstallEvent {
			continue
		}
		if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
			t.Fatalf("reading the skipped-number payload: %v", err)
		}
		found = payload.Kind == mergequeue.SkippedNumbersKind
	}
	if !found {
		t.Fatal("the log holds no row naming the numbers the mint skipped")
	}
	if payload.Seen != 7 || payload.InRecords != 0 || payload.Number != 8 {
		t.Errorf("the row reports seen %d, in records %d, seated at %d", payload.Seen, payload.InRecords, payload.Number)
	}

	// The next mint on the same service skips nothing: the records now run ahead
	// of the store.
	second := queued(ctx, t, pool, token, 2)
	repo.verified[second.ID] = mergequeue.Verified{Commit: "commit-nine", BuildID: "bl_nine", Passed: true}
	pass, err = q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("the second Run: %v", err)
	}
	last := pass.Outcomes[len(pass.Outcomes)-1]
	if last.Release.Number != 9 || len(last.SkippedNumbers) != 0 {
		t.Errorf("the second mint is number %d and skipped %v", last.Release.Number, last.SkippedNumbers)
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
