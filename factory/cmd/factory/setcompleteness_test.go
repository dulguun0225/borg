// What the set answers, checked at the Decomposition row: every requirement in
// force on the intent named by some item or derived into shares, and every
// derived share named by an item.
package main

import (
	"testing"

	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/intent"
)

// TestSetRejectionOverWhatTheSetAnswers is
// ../../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/README.md's
// "A set that leaves a requirement named by no item and derived into none is
// rejected there, and so is one that leaves a derived requirement named by no
// item", and the paragraph after it, "And so is one with an item that answers
// no requirement, whole or derived" — the three directions and the cases that
// are not rejections.
func TestSetRejectionOverWhatTheSetAnswers(t *testing.T) {
	whole := intent.Requirement{ID: "rq_whole", Kind: intent.KindConfirmed, Statement: "the whole"}
	enumerated := intent.Requirement{ID: "rq_enumerated", Kind: intent.KindEnumeratedFromEvidence, Statement: "from evidence"}
	shareA := intent.Requirement{ID: "rq_a", Kind: intent.KindDerived, DerivedFrom: "rq_whole", Statement: "a's share"}
	shareB := intent.Requirement{ID: "rq_b", Kind: intent.KindDerived, DerivedFrom: "rq_whole", Statement: "b's share"}
	unanswerable := intent.Requirement{ID: "rq_none", Kind: intent.KindConfirmed,
		Statement: "nothing answers this", UnanswerableReason: "no service the factory knows can"}

	for _, one := range []struct {
		name     string
		inForce  []intent.Requirement
		answered []string
		members  []gate.SetMember
		rejects  bool
		check    string
	}{
		{
			name:     "one item answers the requirement whole",
			inForce:  []intent.Requirement{whole},
			answered: []string{"rq_whole"},
			members:  []gate.SetMember{{ItemID: "it_a", Requirements: 1}},
		},
		{
			name:     "the split spreads it and every share is answered",
			inForce:  []intent.Requirement{whole, shareA, shareB},
			answered: []string{"rq_a", "rq_b"},
			members:  []gate.SetMember{{ItemID: "it_a", Requirements: 1}, {ItemID: "it_b", Requirements: 1}},
		},
		{
			name:    "a requirement decomposition marked unanswerable is named by no item",
			inForce: []intent.Requirement{unanswerable},
		},
		{
			name:    "a requirement named by no item and derived into none",
			inForce: []intent.Requirement{whole},
			rejects: true,
			check:   gate.AutoRejectedByRequirementNamedByNoItem,
		},
		{
			name:     "a requirement the factory enumerated, named by no item",
			inForce:  []intent.Requirement{whole, enumerated},
			answered: []string{"rq_whole"},
			rejects:  true,
			check:    gate.AutoRejectedByRequirementNamedByNoItem,
		},
		{
			name:     "a derived share named by no item",
			inForce:  []intent.Requirement{whole, shareA, shareB},
			answered: []string{"rq_a"},
			rejects:  true,
			check:    gate.AutoRejectedByDerivedRequirementNamedByNoItem,
		},
		{
			name:     "a member of the set answers no requirement, whole or derived",
			inForce:  []intent.Requirement{whole},
			answered: []string{"rq_whole"},
			members:  []gate.SetMember{{ItemID: "it_a", Requirements: 1}, {ItemID: "it_b", Requirements: 0}},
			rejects:  true,
			check:    gate.AutoRejectedByItemAnsweringNoRequirement,
		},
	} {
		t.Run(one.name, func(t *testing.T) {
			check, found, rejects := setRejection(one.inForce, one.answered, one.members)
			if rejects != one.rejects {
				t.Fatalf("setRejection = %q, %q, %v; want rejects = %v", check, found, rejects, one.rejects)
			}
			if rejects && found == "" {
				t.Error("the rejection carries no reason, and the close event's is what a re-decomposition reads")
			}
			if !rejects && found != "" {
				t.Errorf("a complete set reports %q", found)
			}
			if check != one.check {
				t.Errorf("the rejection names check %q, want %q — the close event carries it as auto_rejected_by",
					check, one.check)
			}
		})
	}
}
