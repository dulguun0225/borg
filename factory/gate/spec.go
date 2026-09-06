package gate

import (
	"fmt"
	"slices"
)

// The Spec row's own mechanical rejection: the two directions this row rejects
// in over the requirement a criterion names, whatever the score returns.

// The checks that reject at the Spec row, in the words a close event names one
// by. They are constants here for the reason the merge row's are: a caller
// cannot report a rejection under a name of its own.
const (
	// AutoRejectedByRequirementUnanswered is a requirement assigned to the item
	// that no criterion in force for it names.
	AutoRejectedByRequirementUnanswered = "a requirement assigned to the item that no criterion in force for it names"
	// AutoRejectedByCriterionElsewhere is a criterion naming a requirement
	// assigned elsewhere.
	AutoRejectedByCriterionElsewhere = "a criterion naming a requirement assigned elsewhere"
)

// SpecChecks is every check that rejects on its own terms at the Spec row, in
// the order the design names them.
var SpecChecks = []string{AutoRejectedByRequirementUnanswered, AutoRejectedByCriterionElsewhere}

// ChecksAt is the checks a row rejects on its own terms. Two rows have any: the
// Spec row over the requirement a criterion names, and the Merge to master row
// over the candidate's run. Every other row rejects only on a verdict.
func ChecksAt(row Row) []string {
	switch row.Kind {
	case KindSpec:
		return SpecChecks
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
// carries the one this returns and the other is found by the next attempt.
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
