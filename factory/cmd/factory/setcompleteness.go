package main

import (
	"fmt"
	"slices"

	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/intent"
)

// What the set answers, checked at the Decomposition row and as mechanically as
// the cycle beside it: a requirement is answered by the item assigned it, or by
// the items answering every requirement derived from it, so the whole the
// requester confirmed is covered exactly where each share is.
//
// It is computed here and not in package gate: the requirements are package
// intent's and what each item answers is package item's, and the gate package
// imports neither's reads — the same division [gate.SpecRejection] already
// takes at the Spec row.

// setRejection is the Decomposition row's completeness check over one intent's
// reading in force and the requirement ids the set's items answer between them.
// It returns which of [gate.DecompositionChecks] rejects and what it found, and
// false where the set is complete.
//
// Both directions the design names are here. A requirement the requester
// confirmed that no item answers and no share was derived from is unanswered,
// unless decomposition marked it unanswerable, which is a requirement named by
// no item on purpose. A derived requirement no item answers is a share the
// split wrote and left with nobody, which reads as an ordinary requirement to
// every gate below.
//
// The first direction is reported first, and a set that fails both carries the
// one this returns; the other is found by the next round. The check name is what
// the close event carries as auto_rejected_by, the way the Spec row's does.
func setRejection(inForce []intent.Requirement, answered []string) (check, found string, rejects bool) {
	for _, r := range inForce {
		if r.Kind == intent.KindDerived || r.Unanswerable() {
			continue
		}
		if slices.Contains(answered, r.ID) || derivedFrom(inForce, r.ID) {
			continue
		}
		return gate.AutoRejectedByRequirementNamedByNoItem,
			fmt.Sprintf("requirement %s is named by no item of the set and derived into none: %s",
				r.ID, r.Statement), true
	}
	for _, r := range inForce {
		if r.Kind != intent.KindDerived || slices.Contains(answered, r.ID) {
			continue
		}
		return gate.AutoRejectedByDerivedRequirementNamedByNoItem,
			fmt.Sprintf("share %s, derived from requirement %s, is named by no item of the set: %s",
				r.ID, r.DerivedFrom, r.Statement), true
	}
	return "", "", false
}

// derivedFrom reports whether the reading in force holds a share derived from
// the requirement named. A requirement spread over the set is assigned to no
// item whole, and this is what says it was spread rather than dropped.
func derivedFrom(inForce []intent.Requirement, requirementID string) bool {
	for _, r := range inForce {
		if r.Kind == intent.KindDerived && r.DerivedFrom == requirementID {
			return true
		}
	}
	return false
}
