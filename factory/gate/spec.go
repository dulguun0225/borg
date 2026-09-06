package gate

import (
	"fmt"
	"slices"
)

// The Spec row's own mechanical rejections: the uncontrolled hazard, and the two
// directions this row rejects in over the requirement a criterion names — each
// rejecting whatever the score returns.

// The checks that reject at the Spec row, in the words a close event names one
// by. They are constants here for the reason the merge row's are: a caller
// cannot report a rejection under a name of its own.
const (
	// AutoRejectedByUncontrolledHazard is a build in an area graded irreversible
	// with no criterion in force naming that area's hazardous operation. What
	// computes it is package criterion, which holds the provenance field the
	// query reads.
	AutoRejectedByUncontrolledHazard = "a build in an irreversible area with no criterion in force naming its hazardous operation"
	// AutoRejectedByRequirementUnanswered is a requirement assigned to the item
	// that no criterion in force for it names.
	AutoRejectedByRequirementUnanswered = "a requirement assigned to the item that no criterion in force for it names"
	// AutoRejectedByCriterionElsewhere is a criterion naming a requirement
	// assigned elsewhere.
	AutoRejectedByCriterionElsewhere = "a criterion naming a requirement assigned elsewhere"
)

// SpecChecks is every check that rejects on its own terms at the Spec row.
//
// The order is the order a caller that computes more than one reports them in,
// and the hazard is first: it is a fact about the whole build, where the two
// beside it are about one requirement each, and the drafting stage answers it by
// deriving the criterion the area wants. A firing that fails more than one
// carries the first and the rest are found by the next attempt.
var SpecChecks = []string{
	AutoRejectedByUncontrolledHazard,
	AutoRejectedByRequirementUnanswered,
	AutoRejectedByCriterionElsewhere,
}

// ChecksAt is the checks a row rejects on its own terms. Four rows have any:
// the Decomposition row over what the set answers, the Spec row over the hazard
// and the requirement a criterion names, the Implementation row over the
// screens, and the Merge to master row over the candidate's run. Every other row
// rejects only on a verdict.
func ChecksAt(row Row) []string {
	switch row.Kind {
	case KindDecomposition:
		return DecompositionChecks
	case KindSpec:
		return SpecChecks
	case KindImplementation:
		return ImplementationChecks
	case KindMergeToMaster:
		return MechanicalChecks
	default:
		return nil
	}
}

// SpecRejection is the Spec row's rejection in both directions, computed over
// two lists of requirement ids: assigned is every requirement decomposition
// assigned the item, and named is the requirement each criterion in force for
// the item names, one entry per criterion and empty where a criterion names
// none.
//
// It returns which of [SpecChecks] rejects and what it found, and false where
// neither direction does. The first direction is reported first, an unanswered
// requirement being what a set is incomplete about; a firing that fails both
// carries the one this returns and the other is found by the next attempt. The
// uncontrolled hazard is not read here and is not one of the two: it is a query
// over the criterion table, which the caller makes before this one.
//
// What computes the two lists is the caller: the requirements are package
// intent's and the criteria are package criterion's, and this package imports
// neither's reads.
func SpecRejection(assigned, named []string) (check, found string, rejects bool) {
	for _, requirement := range assigned {
		if !slices.Contains(named, requirement) {
			return AutoRejectedByRequirementUnanswered,
				fmt.Sprintf("requirement %s is assigned to this item and no criterion in force for it names it", requirement),
				true
		}
	}
	for _, requirement := range named {
		if requirement == "" || slices.Contains(assigned, requirement) {
			continue
		}
		return AutoRejectedByCriterionElsewhere,
			fmt.Sprintf("a criterion names requirement %s, which is not assigned to this item", requirement),
			true
	}
	return "", "", false
}
