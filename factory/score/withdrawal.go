package score

import "context"

// What a spec version under decision removes, which resolves at the Spec row:
// the withdrawal of a criterion whose provenance names an authority, and a
// superseding screen state machine that declares a transition the
// human-confirmed machine it supersedes did not.

// The three provenances that name an authority. Withdrawing a criterion of any
// of them is not silent: the decision is a human's whatever the formula returns,
// routed to the human that provenance names rather than to the owner by default.
const (
	// ProvenanceHumanConfirmed is a criterion a human confirmed at the Spec row.
	// The human its provenance names is the actor of the introducing decision,
	// or another holder of that row's duty where that actor no longer holds it.
	ProvenanceHumanConfirmed = "human-confirmed"
	// ProvenanceConstraintDerived is a criterion derived from a constraint. The
	// human is whoever holds duty 2 over that constraint, the named human a
	// safeguard's routing field gives where one names it, and the owner where
	// the constraint is withdrawn and nobody holds it.
	ProvenanceConstraintDerived = "constraint-derived"
	// ProvenanceHazardDerived is a criterion derived from an area's hazard
	// severity. The human is whoever holds duty 9 over that area, and the owner
	// where nobody does.
	ProvenanceHazardDerived = "hazard-derived"
)

// The two things a spec version removes that resolve at Spec, in the words the
// resolution and the vector carry.
const (
	// RemovedCriterion is a criterion of one of the three provenances withdrawn
	// by the version under decision.
	RemovedCriterion = "a criterion whose provenance names an authority is withdrawn"
	// RemovedScreenTransition is a superseding screen state machine declaring a
	// transition a human-confirmed machine did not, which under a closed machine
	// is what admits behaviour the confirmed one forbade.
	RemovedScreenTransition = "a superseding screen state machine admits what a human-confirmed one forbade"
)

// ProtectionRemoved is one such removal: which of the two it is, the record it
// is over, the provenance that names the authority, and the human that
// provenance names. The last is carried so that the decision can be routed to
// them rather than to the owner by default; nothing here routes.
type ProtectionRemoved struct {
	What string `json:"what"`
	// SubjectID is the criterion withdrawn, or the human-confirmed machine the
	// version under decision supersedes.
	SubjectID string `json:"subject_id"`
	// Provenance is one of [ProvenanceHumanConfirmed],
	// [ProvenanceConstraintDerived] and [ProvenanceHazardDerived]. A screen
	// revision names the first, a human-confirmed machine being the only one
	// whose transitions this reads.
	Provenance string `json:"provenance"`
	// RoutedTo is the per-person key the provenance names, and is empty where it
	// names nobody the factory can resolve, which routes to the owner.
	RoutedTo string `json:"routed_to,omitempty"`
}

// Withdrawals is what reads them. It is an interface because both readings are
// queries over records this package does not read — a criterion's provenance and
// the withdrawals of one spec version, and the transitions two screen state
// machines declare — so the composition hands the score whatever performs them.
type Withdrawals interface {
	// ProtectionRemovedBy is what the spec version under decision removes, and
	// nothing for a version that removes none and for a firing naming no version.
	ProtectionRemovedBy(ctx context.Context, artifactID string) ([]ProtectionRemoved, error)
}

// NoWithdrawals reads nothing removed. It is the value a composition with no
// such reader hands in, and what [New] composes for a nil one. It is not the
// same as a reader that failed: that would be an unavailable input and would
// resolve the factor, and this is an empty one — the distinction [NoMarks]
// already keeps.
type NoWithdrawals struct{}

// ProtectionRemovedBy is nothing.
func (NoWithdrawals) ProtectionRemovedBy(context.Context, string) ([]ProtectionRemoved, error) {
	return nil, nil
}
