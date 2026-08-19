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
	// MergeToMaster is the release event: where a candidate becomes a numbered
	// release, and where the verdict on the candidate is given.
	MergeToMaster Row = "merge_to_master"
	// DeployToProduction is the last row before a release takes traffic, and the
	// one that offers hold.
	DeployToProduction Row = "deploy_to_production"
)

// Rows is every row this package fires.
var Rows = []Row{MergeToMaster, DeployToProduction}

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
	VerdictHold Verdict = "hold"
)

// ErrRowUnknown is returned for a row outside [Rows].
var ErrRowUnknown = errors.New("gate: not a gate row this milestone builds")

// Actions is what may be done at one row. The merge row approves or rejects;
// the production deploy row approves or holds, the merge having happened and the
// number being already assigned, so there is nothing left to reject to.
func Actions(row Row) ([]Verdict, error) {
	switch row {
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
// human deciding there is performing UAT. The production deploy row names no
// duty, because the design names none for it — a human there is deciding whether
// the release goes live and not verifying an artifact, and inventing a duty
// number for the row would put a claim on the opening row that the tree does not
// make. What the row waits on is whoever holds it, and the surface that shows a
// duty holder their rows is M7's.
func WaitsOn(row Row) string {
	switch row {
	case MergeToMaster:
		return "duty 7, UAT — the owner"
	case DeployToProduction:
		return "whoever holds this gate row — the owner, under no numbered duty of its own"
	default:
		return ""
	}
}

// ReturnsTo is the stage a reject sends the item back to where the verdict names
// none. Both event rows that reject default to Implementation, there being no
// stage of their own and none between; the deploy row does not reject at all.
const ReturnsTo = "implementation"

// ErrStrategyPinRefused is returned for the production deploy row's third
// action. A strategy that keeps a control needs a substrate that decides what
// share of arriving traffic reaches each of two builds; a target that runs a
// release as a local process moves a process instead, so the row is unavailable
// here and every deploy is straight — the same exemption a service's first
// release already takes, arriving for the whole install rather than for one
// release.
var ErrStrategyPinRefused = errors.New("gate: no strategy to pin — this substrate moves a process rather than traffic, so every deploy is straight")
