package service

import "github.com/dulguun0225/borg/factory/gatepolicy"

// The shipped defaults of the four fields the design fixes rather than gives a
// number for: a value the product ships, read wherever the field stands
// unauthored, the way every other supplied value is — [gatepolicy.Authored.Or]
// is what already reads an authored value or a value from elsewhere, and a
// fixed default is such a value, computed at compile time rather than at a
// gate. The design states no number for any of the four, so each constant here
// is this package's own choice and not a figure the document names; a reader
// of a decision taken under one reaches it through the release that shipped
// it.
const (
	// ShippedMutantCap is how many mutants Implementation's mutation score may
	// spend per item where an owner authored none.
	ShippedMutantCap = 50
	// ShippedFailureRecordKeyCap is how many distinct keys a release may hold
	// open per interval for its failure records where an owner authored none.
	ShippedFailureRecordKeyCap = 20
	// ShippedUnreliableBound is the rate of disagreement above which a
	// criterion is unreliable where an owner authored no bound.
	ShippedUnreliableBound = 0.2
	// ShippedIncidentItemBoundSeconds is how long an incident-raised item may
	// be worked before a human is reached, in seconds, where an owner
	// authored no bound. Three days.
	ShippedIncidentItemBoundSeconds = 3 * 24 * 60 * 60
)

// MutantCapInForce is the mutant cap in force: the value an owner authored, or
// [ShippedMutantCap] where they authored none.
func MutantCapInForce(a gatepolicy.Authored) float64 { return a.Or(ShippedMutantCap) }

// FailureRecordKeyCapInForce is the failure-record key cap in force, read the
// same way.
func FailureRecordKeyCapInForce(a gatepolicy.Authored) float64 {
	return a.Or(ShippedFailureRecordKeyCap)
}

// UnreliableBoundInForce is the unreliable bound in force, read the same way.
// It is what a caller hands [criterion.Unreliable] rather than the raw column:
// an unauthored bound read as zero would mark a criterion unreliable at its
// first disagreement, which is not what "nothing authored" means for this
// field.
func UnreliableBoundInForce(a gatepolicy.Authored) float64 { return a.Or(ShippedUnreliableBound) }

// IncidentItemBoundSecondsInForce is the incident-raised item bound in force,
// in seconds, read the same way.
func IncidentItemBoundSecondsInForce(a gatepolicy.Authored) float64 {
	return a.Or(ShippedIncidentItemBoundSeconds)
}
