package mergequeue

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
)

// A commit on master no build of a candidate names is a commit the queue did not
// put there, and the service's merges are held until the wait it opened ends. The
// wait ends one of two ways: master returned to the commit the newest release
// names, which is repository administration and which the queue reads at its next
// pass, or a human accepts the commit here.

// Acceptance is what a human's acceptance of such a commit produced: the release
// minted over it, naming a build and no item, or the failure that left the wait
// standing.
type Acceptance struct {
	ServiceID string
	Commit    string
	Release   release.Release
	BuildID   string
	Published []contract.Published
	// SkippedNumbers is the numbers the mint passed over, as at any other mint.
	SkippedNumbers []int64
	// Why is what the re-verification found where it failed, in which case
	// nothing was minted.
	Why string
	// RejectionRow is the row the failure was written as, and WaitRow is the wait
	// the acceptance closed or, on a failure, left standing.
	RejectionRow string
	WaitRow      string
}

// Minted reports whether the acceptance produced a release.
func (a Acceptance) Minted() bool { return a.Release.ID != "" }

// AcceptCommit is a human at Work accepting a commit the queue did not make. The
// queue builds it, re-verifies it as it re-verifies a candidate — the contract
// checks included — and mints its release in master's order, naming the build and
// no item. A re-verification that fails leaves the wait standing with the failure
// on it.
//
// The contract version each of the commit's interfaces publishes is written in
// the same write as the release, as at a fast-forward: the producer's contract
// diff is among the checks the re-verification ran, so a consumer reaching an
// interface the commit changed reads a version naming that release.
//
// A release naming no item is one no gate decided: no consumer contract is
// derived for it, since none is authored at a stage it never passed, its
// authorship rollup names nothing the factory wrote, no prior moves, and every
// traversal from it ends at the acceptance rather than at an intent. That is what
// makes the claim that what was verified is what ships one a reader checks per
// release.
func (q *Queue) AcceptCommit(ctx context.Context, human record.Actor, serviceID, commit string) (Acceptance, error) {
	if err := human.Validate(); err != nil {
		return Acceptance{}, err
	}
	if human.Kind != record.KindHuman {
		return Acceptance{}, fmt.Errorf("%w: %s is a %s", ErrNotAHuman, human.Key, human.Kind)
	}
	if serviceID == "" {
		return Acceptance{}, ErrServiceIDEmpty
	}
	if commit == "" {
		return Acceptance{}, ErrCommitEmpty
	}
	accepted := Acceptance{ServiceID: serviceID, Commit: commit}

	unlock, err := q.lock(ctx, serviceID)
	if err != nil {
		return accepted, err
	}
	defer unlock()

	open, err := q.standing(ctx)
	if err != nil {
		return accepted, err
	}
	wait, found := open[subject{
		Kind: WaitMasterHoldsACommitTheQueueDidNotMake, ServiceID: serviceID, Commit: commit,
	}]
	if !found {
		return accepted, fmt.Errorf("%w: %s of %s", ErrNoWaitStanding, commit, serviceID)
	}
	accepted.WaitRow = wait.ID

	verified, err := q.repo.VerifyCommit(ctx, serviceID, commit)
	if err != nil {
		return accepted, fmt.Errorf("mergequeue: re-verifying the accepted commit %s of %s: %w",
			commit, serviceID, err)
	}
	accepted.BuildID = verified.BuildID
	if !verified.Passed {
		accepted.Why = verified.Why
		row, err := q.rejectCommit(ctx, serviceID, commit, verified)
		if err != nil {
			return accepted, err
		}
		accepted.RejectionRow = row
		return accepted, nil
	}
	if verified.BuildID == "" || verified.Commit != commit {
		return accepted, fmt.Errorf(
			"mergequeue: the re-verification of %s of %s passed and names commit %q, build %q",
			commit, serviceID, verified.Commit, verified.BuildID)
	}

	outcome, err := q.mint(ctx, serviceID, "", verified)
	if err != nil {
		return accepted, err
	}
	accepted.Release, accepted.Published = outcome.Release, outcome.Published
	accepted.SkippedNumbers = outcome.SkippedNumbers

	// The closing names the human as actor: the acceptance is theirs, and the log
	// row naming them is what the design puts at this end of the wait.
	if _, err := q.log.AppendWaitClose(ctx, decisionlog.Entry{
		Actor: human, Payload: wait.Payload, FormatVersion: waitFormatVersion, Closes: wait.ID,
	}); err != nil {
		return accepted, err
	}
	return accepted, nil
}

// rejectCommit is the row a failed re-verification of an accepted commit leaves.
// It names no item and no reading: there is no item to send anywhere, so no stage
// is named and no attempt is counted, and there is no earlier run of the criteria
// on a candidate environment for a confirming run to disagree with. The wait
// stands, with this row against it.
func (q *Queue) rejectCommit(ctx context.Context, serviceID, commit string, verified Verified) (string, error) {
	payload, err := json.Marshal(RejectionPayload{
		Kind:      RejectionKind,
		ServiceID: serviceID,
		BuildID:   verified.BuildID,
		Commit:    commit,
		Why:       verified.Why,
	})
	if err != nil {
		return "", fmt.Errorf("mergequeue: marshalling the rejection of %s: %w", commit, err)
	}
	row, err := q.log.AppendQueueRejection(ctx, decisionlog.Entry{
		Actor: Actor, Payload: string(payload), FormatVersion: rejectionFormatVersion,
	})
	if err != nil {
		return "", err
	}
	return row.ID, nil
}
