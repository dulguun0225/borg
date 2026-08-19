package gate

import (
	"errors"
	"fmt"
	"slices"
)

// Row is one of the gate rows this milestone builds. doc.go says which of the
// design's rows are not here and what each waits for.
type Row string

const (
	// DeployToCandidateEnvironment is where the candidate's own environment is
	// created and the candidate's build is put on it. What the deploy provides is
	// the criteria: nothing else attaches to this row — no strategy, no rollout,
	// no watch window — because a candidate environment has no organic traffic.
	DeployToCandidateEnvironment Row = "deploy_to_candidate_environment"
	// MergeToMaster is the release event: where a candidate becomes a numbered
	// release, and where the verdict on the candidate is given. Approving it
	// admits the candidate to the merge queue.
	MergeToMaster Row = "merge_to_master"
	// DeployToProduction is the last row before a release takes traffic, and the
	// one that offers hold and no reject.
	DeployToProduction Row = "deploy_to_production"
)

// Rows is every row this package fires, in the order the path reaches them.
var Rows = []Row{DeployToCandidateEnvironment, MergeToMaster, DeployToProduction}

// Verdict is what a decision closes with.
type Verdict string

const (
	// VerdictApprove admits the event. At the merge row the caller performs the
	// merge itself, there being no merge queue yet.
	VerdictApprove Verdict = "approve"
	// VerdictReject sends the item back up the pipeline and requires feedback.
	// It is available up to the merge to master and nowhere after it.
	VerdictReject Verdict = "reject"
	// VerdictHold leaves the event queued with the change still good. It counts
	// no attempt and teaches the score nothing, and only a deploy row offers it.
	// This is the hold a human sets, which is the one of the design's three
	// written as a decision; the factory's own is [HoldDependencyNotLive] or
	// [HoldNoRoomForAnotherEnvironment], and neither is a verdict.
	VerdictHold Verdict = "hold"
)

// ErrRowUnknown is returned for a row outside [Rows].
var ErrRowUnknown = errors.New("gate: not a gate row this milestone builds")

// Actions is what may be done at one row. The candidate deploy row approves,
// holds, or rejects — it is fed from a candidate, and reject is available up to
// the merge to master; the merge row approves or rejects; the production deploy
// row approves or holds, the merge having happened and the number being already
// assigned, so there is nothing left to reject to.
func Actions(row Row) ([]Verdict, error) {
	switch row {
	case DeployToCandidateEnvironment:
		return []Verdict{VerdictApprove, VerdictHold, VerdictReject}, nil
	case MergeToMaster:
		return []Verdict{VerdictApprove, VerdictReject}, nil
	case DeployToProduction:
		return []Verdict{VerdictApprove, VerdictHold}, nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrRowUnknown, row)
	}
}

// permits reports whether the row offers the verdict.
func permits(row Row, verdict Verdict) error {
	actions, err := Actions(row)
	if err != nil {
		return err
	}
	if !slices.Contains(actions, verdict) {
		return fmt.Errorf("%w: %s offers %v", ErrVerdictUnknown, row, actions)
	}
	return nil
}

// WaitsOn is what an opening row waits on: the duty a human at it is performing,
// or the holder where the design names no duty. It is on the opening row so a
// reader of a pending decision knows who the verdict is waited on from, and it is
// empty on an auto-pass because nothing is waited on.
//
// The merge row names duty 7, which the design says of it in as many words: a
// human deciding there is performing UAT. Neither deploy row names a duty,
// because the design names none for either — a human at one is deciding whether
// the deploy happens and not verifying an artifact, and inventing a duty number
// for a row would put a claim on the opening row that the tree does not make.
// What such a row waits on is whoever holds it, and the surface that shows a
// duty holder their rows is M7's.
func WaitsOn(row Row) string {
	switch row {
	case MergeToMaster:
		return "duty 7, UAT — the owner"
	case DeployToCandidateEnvironment, DeployToProduction:
		return "whoever holds this gate row — the owner, under no numbered duty of its own"
	default:
		return ""
	}
}

// ReturnsTo is the stage a reject sends the item back to where the verdict names
// none. Both rows that reject default to Implementation, there being no stage of
// their own and none between; the production deploy row does not reject at all.
const ReturnsTo = "implementation"

// The conditions the factory's own hold is computed from, in the words a caller
// reports it with. A hold of this kind is not a verdict and writes nothing: it is
// recomputed at every firing, because a record for it would be a decision where
// the design says nothing is decided, and re-testing would append one every time
// the gate re-fired.
//
// They are constants here and computed by the caller. What computes them reads the
// item's declared dependencies and the deploy records of their services, and this
// package imports neither — what it owns is the vocabulary, so a caller cannot
// report a hold under a name of its own.
const (
	// HoldDependencyNotLive is a declared dependency that is not its service's
	// current release. At the candidate deploy row the question is whether it is
	// live at all, the environment being composed from it; at the production
	// deploy row, whether it is live still — a producer that was live when its
	// consumer verified can be rolled back before that consumer deploys.
	HoldDependencyNotLive = "a declared dependency is not its service's current release"
	// HoldNoRoomForAnotherEnvironment is the substrate with no room for another
	// candidate environment. It is the one condition at the candidate deploy row
	// the cut could not have declared, and the one hold here that is written
	// anywhere: it is not a record and no parameter of an owner's limits it, so it
	// goes into the log as a wait, with the component that met it as the actor.
	HoldNoRoomForAnotherEnvironment = "the substrate has no room for another candidate environment"
)

// ErrStrategyPinRefused is returned for the production deploy row's third
// action. A strategy that keeps a control needs a substrate that decides what
// share of arriving traffic reaches each of two builds; a target that runs a
// release as a local process moves a process instead, so the row is unavailable
// here and every deploy is straight — the same exemption a service's first
// release already takes, arriving for the whole install rather than for one
// release.
var ErrStrategyPinRefused = errors.New("gate: no strategy to pin — this substrate moves a process rather than traffic, so every deploy is straight")
