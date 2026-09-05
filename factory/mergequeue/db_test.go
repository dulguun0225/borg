package mergequeue_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/mergequeue"
	"github.com/dulguun0225/borg/factory/release"
)

// TestRunMintsOnAPassAndRejectsOnAFailure is the queue's two outcomes over one
// service: a candidate that passes its re-verification fast-forwards and is
// minted a release naming the build that re-verification produced; one that
// fails is rejected with a rejection row saying why, which reading it was, and
// that an attempt is counted at Implementation.
//
// Neither transition is written here. The queue's row in components.md names no
// dispatch, so the item's advance to merged and its return to Implementation are
// the caller's writes, and what the queue hands the caller is the outcome.
func TestRunMintsOnAPassAndRejectsOnAFailure(t *testing.T) {
	repo := newRepository()
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{Repository: repo})

	passes := queued(ctx, t, pool, token, 1)
	fails := queued(ctx, t, pool, token, 2)
	repo.verified[passes.ID] = mergequeue.Verified{Commit: "commit-one", BuildID: "bl_one", Passed: true}
	repo.verified[fails.ID] = mergequeue.Verified{Commit: "commit-two", BuildID: "bl_two",
		Why: "criterion cr_a is failed against build bl_two"}

	pass, err := q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(pass.Outcomes) != 2 {
		t.Fatalf("Run returned %d outcomes, two candidates were queued: %+v", len(pass.Outcomes), pass.Outcomes)
	}

	merged, rejected := pass.Outcomes[0], pass.Outcomes[1]
	if merged.ItemID != passes.ID || !merged.Merged {
		t.Fatalf("the first outcome is %+v, want %s merged", merged, passes.ID)
	}
	if merged.Release.Number != 1 || merged.Release.BuildID != "bl_one" || merged.Release.Commit != "commit-one" {
		t.Errorf("the release is number %d of build %s at commit %s, want 1 of bl_one at commit-one",
			merged.Release.Number, merged.Release.BuildID, merged.Release.Commit)
	}
	if merged.Release.Actor != mergequeue.Actor {
		t.Errorf("the release was minted by %+v, want the queue", merged.Release.Actor)
	}
	if !merged.Release.NamesAnItem() {
		t.Error("the release names no item, and a fast-forward's release names the item that caused it")
	}
	if len(repo.fastForwards) != 1 || repo.fastForwards[0] != "commit-one" {
		t.Errorf("the fast-forwards were %v, want the commit that was verified", repo.fastForwards)
	}
	if read, err := item.Get(ctx, pool, passes.ID); err != nil || read.Stage != item.StageQueued {
		t.Errorf("the merged item is at %v, %v — the queue writes no stage, its caller does", read.Stage, err)
	}

	if rejected.ItemID != fails.ID || rejected.Merged {
		t.Fatalf("the second outcome is %+v, want %s rejected", rejected, fails.ID)
	}
	if rejected.Rejection.Row == "" || rejected.Why == "" {
		t.Errorf("the rejection reports row %q and reason %q", rejected.Rejection.Row, rejected.Why)
	}
	if rejected.Rejection.ReturnsTo != gate.ReturnsToImplementation || !rejected.Rejection.CountsAnAttempt {
		t.Errorf("the rejection returns the item to %q and counts an attempt %v, want Implementation and true",
			rejected.Rejection.ReturnsTo, rejected.Rejection.CountsAnAttempt)
	}

	// Only one release exists: the rejection mints none, which is what makes a
	// number record a change that was accepted.
	var releases int
	if err := pool.QueryRow(ctx, `select count(*) from `+release.Table).Scan(&releases); err != nil {
		t.Fatalf("counting releases: %v", err)
	}
	if releases != 1 {
		t.Errorf("%d releases exist, one candidate passed", releases)
	}

	// The rejection is its own shape the log wrote with the queue as caller and
	// actor: no gate fired, the Merge to master gate's own having closed as an
	// approval.
	rows := readLog(t, ctx, pool, token)
	var rejection decisionlog.Row
	found := false
	for _, row := range rows {
		if row.Shape == decisionlog.ShapeQueueRejection {
			rejection, found = row, true
		}
	}
	if !found {
		t.Fatalf("the log holds %+v, want a queue rejection among them", rows)
	}
	if rejection.Actor != mergequeue.Actor {
		t.Errorf("the row was written as %+v, want the queue", rejection.Actor)
	}
	var payload mergequeue.RejectionPayload
	if err := json.Unmarshal([]byte(rejection.Payload), &payload); err != nil {
		t.Fatalf("reading the rejection payload: %v", err)
	}
	if payload.Kind != mergequeue.RejectionKind || payload.ItemID != fails.ID {
		t.Errorf("the payload is %+v, want kind %q for item %s", payload, mergequeue.RejectionKind, fails.ID)
	}
	if payload.ReturnsTo != string(gate.ReturnsToImplementation) || !payload.CountsAnAttempt {
		t.Errorf("the payload returns the item to %q and counts an attempt %v", payload.ReturnsTo, payload.CountsAnAttempt)
	}
	if payload.Reading == "" {
		t.Error("the payload names no reading, and the rejection names which of the three it was")
	}
	if err := decisionlog.NewReader(pool, token).Verify(ctx, testActor); err != nil {
		t.Errorf("the chain does not verify: %v", err)
	}
}

// TestTheOrderIsTheTierThenThePriorityThenTheApproval: the queue's order is the
// tier the requester proposed, then the priority an owner set within it, then the
// time of the merge approval in the log. Reordering changes when a candidate
// re-verifies and never what it has to pass.
func TestTheOrderIsTheTierThenThePriorityThenTheApproval(t *testing.T) {
	repo := newRepository()
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{Repository: repo})

	first := queued(ctx, t, pool, token, 1)
	second := queued(ctx, t, pool, token, 2)
	third := queued(ctx, t, pool, token, 3)
	// A fourth whose intent the requester proposed a tier for, which orders ahead
	// of every priority.
	tiered := queuedOf(ctx, t, pool, token, requested(ctx, t, pool, token, 4, 3), 4)

	members, err := q.Members(ctx, serviceID)
	if err != nil {
		t.Fatalf("Members: %v", err)
	}
	if len(members) != 4 || members[0].ID != tiered.ID {
		t.Fatalf("the members are %v, want the tiered item %s first", ids(members), tiered.ID)
	}
	if members[1].ID != first.ID || members[3].ID != third.ID {
		t.Errorf("the untiered members are %v, want them in the order they were decomposed", ids(members[1:]))
	}

	// An owner pushes the last one to the front. It goes ahead of the other
	// untiered members and behind the tiered one: a priority orders within a tier.
	if _, err := item.NewDispatch(pool, token).SetPriority(ctx, owner, third.ID, 5); err != nil {
		t.Fatalf("SetPriority: %v", err)
	}
	if members, err = q.Members(ctx, serviceID); err != nil {
		t.Fatalf("Members after the priority: %v", err)
	}
	if members[0].ID != tiered.ID || members[1].ID != third.ID {
		t.Errorf("the members are %v, want the tier ahead of the pushed item %s", ids(members), third.ID)
	}

	// Nothing here fires a gate, so no approval time is in the log and the order
	// falls back to decomposition's. What the approval time does to the order is
	// package gate's own demonstration, that being where the payload's shape
	// lives.
	_ = second

	for _, member := range members {
		repo.verified[member.ID] = mergequeue.Verified{
			Commit: "commit-" + member.ID, BuildID: "bl_" + member.ID, Passed: true,
		}
	}
	if _, err := q.Run(ctx, serviceID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(repo.reverified) != 4 || repo.reverified[0] != tiered.ID || repo.reverified[1] != third.ID {
		t.Errorf("the queue re-verified %v, want the tier and then the pushed item", repo.reverified)
	}
	// The numbers follow the order the queue merged in, which is what makes a
	// number order builds.
	highest, found, err := release.Highest(ctx, pool, serviceID)
	if err != nil || !found {
		t.Fatalf("Highest = found %v, %v", found, err)
	}
	if highest.Number != 4 {
		t.Errorf("the highest number is %d after four merges", highest.Number)
	}
}

// TestAnEmptyQueueAndAnUnnamedServiceAreRefusedOrEmpty: the order is per service,
// so a run naming none is an error, and a service with nothing queued is no
// outcomes and no error.
func TestAnEmptyQueueAndAnUnnamedServiceAreRefusedOrEmpty(t *testing.T) {
	repo := newRepository()
	ctx, _, _, q := newQueue(t, mergequeue.Composition{Repository: repo})

	if _, err := q.Run(ctx, ""); !errors.Is(err, mergequeue.ErrServiceIDEmpty) {
		t.Errorf("Run naming no service = %v, want ErrServiceIDEmpty", err)
	}
	if _, err := q.Members(ctx, ""); !errors.Is(err, mergequeue.ErrServiceIDEmpty) {
		t.Errorf("Members naming no service = %v, want ErrServiceIDEmpty", err)
	}
	pass, err := q.Run(ctx, serviceID)
	if err != nil || len(pass.Outcomes) != 0 {
		t.Errorf("Run over an empty queue = %+v, %v", pass, err)
	}
}

// TestAReverificationErrorStopsTheRun: a repository that cannot be read is
// infrastructure and not a candidate failing on its merits, so it stops the run
// rather than rejecting the item — nothing is minted and nothing is sent back.
func TestAReverificationErrorStopsTheRun(t *testing.T) {
	unreachable := errors.New("the repository is unreadable")
	repo := newRepository()
	repo.err = unreachable
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{Repository: repo})
	it := queued(ctx, t, pool, token, 1)

	if _, err := q.Run(ctx, serviceID); !errors.Is(err, unreachable) {
		t.Fatalf("Run = %v, want the repository's error", err)
	}
	read, err := item.Get(ctx, pool, it.ID)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}
	if read.Stage != item.StageQueued {
		t.Errorf("the item is at %s, and an infrastructure failure leaves it in the queue", read.Stage)
	}
	// Nothing about the candidate was decided: every row still standing is a read
	// event, the membership's own read of the approval times among them.
	rows := readLog(t, ctx, pool, token)
	for _, row := range rows {
		if row.Shape != decisionlog.ShapeReadEvent {
			t.Errorf("the log holds %+v, and nothing about the candidate was decided", rows)
			break
		}
	}
}

// TestTheLockKeyIsNotTheMints is why the key is derived from this package's own
// name: the run holds a session-level lock across the fast-forward and the mint,
// and the mint takes a lock of its own inside a transaction on another connection.
// One key for both would be a deadlock the pool could not resolve.
func TestTheLockKeyIsNotTheMints(t *testing.T) {
	if mergequeue.AdvisoryLockKey(serviceID) == release.AdvisoryLockKey(serviceID) {
		t.Fatal("the queue's lock key is the mint's, and the run holds one while the mint waits for the other")
	}
	if mergequeue.AdvisoryLockKey("svc_a") == mergequeue.AdvisoryLockKey("svc_b") {
		t.Error("two services share one key, and their merges have nothing to serialise against each other for")
	}
	if key := mergequeue.AdvisoryLockKey(serviceID); key < 0 {
		t.Errorf("the key is %d, and the top bit is cleared so the value is positive", key)
	}
}

// TestAMemberThatAlreadyHasAReleaseIsFinishedNotReverified is the half-write a
// merge can leave: the fast-forward and the mint landed and the caller's advance
// did not. Re-verifying that member would fast-forward to the commit master is
// already at and mint a second number for one merge — a release being unique on
// the service and the number and not on the item — so it is finished instead.
func TestAMemberThatAlreadyHasAReleaseIsFinishedNotReverified(t *testing.T) {
	repo := newRepository()
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{Repository: repo})
	it := queued(ctx, t, pool, token, 1)

	// The state a failed advance leaves: a release for an item still at queued,
	// with master holding its commit.
	minted, err := release.NewWriter(pool, token).Mint(ctx, mergequeue.Actor, release.Minting{
		ServiceID: serviceID, BuildID: "bl_one", Commit: "commit-one", ItemID: it.ID,
	})
	if err != nil {
		t.Fatalf("minting the release the advance did not follow: %v", err)
	}
	repo.head, repo.held["commit-one"] = "commit-one", true

	pass, err := q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(pass.Outcomes) != 1 || !pass.Outcomes[0].Merged {
		t.Fatalf("Run returned %+v, want the member finished as merged", pass.Outcomes)
	}
	if pass.Outcomes[0].Release.ID != minted.ID {
		t.Errorf("the outcome names release %s, want the one already minted, %s",
			pass.Outcomes[0].Release.ID, minted.ID)
	}
	if len(repo.reverified) != 0 {
		t.Errorf("the queue re-verified %v, and a member that already has a release is finished rather than verified again",
			repo.reverified)
	}
	if len(repo.fastForwards) != 0 {
		t.Errorf("the queue fast-forwarded %v, and master is already at that commit", repo.fastForwards)
	}

	var releases int
	if err := pool.QueryRow(ctx, `select count(*) from `+release.Table+` where item_id = $1`, it.ID).Scan(&releases); err != nil {
		t.Fatalf("counting the item's releases: %v", err)
	}
	if releases != 1 {
		t.Errorf("the item has %d releases, and one merge is one number", releases)
	}
}
