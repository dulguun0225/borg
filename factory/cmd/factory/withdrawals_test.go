// What a spec version under decision removes, read on an install where no spec
// version was ever decided by a human — the steady state the design describes,
// the score auto-passing the Spec row.
package main

import (
	"testing"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/service"
)

// TestAConstraintOrHazardDerivedWithdrawalResolvesWithNoVersionHumanConfirmed:
// two of the three provenances are columns of the criterion's own row, so
// withdrawing one is a resolved factor at the Spec row whether or not any spec
// version was ever human-approved. Only the human-confirmed provenance and the
// screen supersession are queries over a decision, and only those two go
// unanswered where the log holds no human's approval at Spec.
func TestAConstraintOrHazardDerivedWithdrawalResolvesWithNoVersionHumanConfirmed(t *testing.T) {
	ctx, d, _ := newPath(t, "")
	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path: %v", err)
	}
	svc, found, err := service.ByName(ctx, d.pool, theService)
	if err != nil || !found {
		t.Fatalf("reading service %s: found %v, %v", theService, found, err)
	}

	by := artifact.By{Authorship: artifact.AuthorshipAgent, Author: d.modelName}
	introducing, introduced, _, err := p.store.SubmitSpec(ctx, p.specAuthorActor(), by,
		"it_withdrawals", svc.ID, "the spec that introduces them",
		[]criterion.Draft{
			{
				Sentence:          "When a payout is requested, the system shall record it.",
				RequirementID:     "rq_constraint",
				ConstraintDerived: []string{"cn_records"},
			},
			{
				Sentence:      "When a ledger entry is voided, the system shall record the reversal.",
				RequirementID: "rq_hazard",
				HazardDerived: "ar_ledger",
			},
			{
				Sentence:      "When a report is asked for, the system shall answer it.",
				RequirementID: "rq_drafted",
			},
		}, nil, nil, "")
	if err != nil {
		t.Fatalf("submitting the spec version that introduces them: %v", err)
	}
	if len(introduced) != 3 {
		t.Fatalf("the version introduced %d criteria, want three", len(introduced))
	}

	withdrawn := make([]string, 0, len(introduced))
	for _, one := range introduced {
		withdrawn = append(withdrawn, one.ID)
	}
	withdrawing, _, _, err := p.store.SubmitSpec(ctx, p.specAuthorActor(), by,
		"it_withdrawals", svc.ID, "the spec that withdraws them", nil, withdrawn, nil, "")
	if err != nil {
		t.Fatalf("submitting the withdrawing spec version: %v", err)
	}
	if introducing.ID == withdrawing.ID {
		t.Fatalf("the two submissions are one version, %s", introducing.ID)
	}

	removed, err := (withdrawals{pool: d.pool, token: d.token}).ProtectionRemovedBy(ctx, withdrawing.ID)
	if err != nil {
		t.Fatalf("reading what the version removes: %v", err)
	}
	if len(removed) != 2 {
		t.Fatalf("the version removes %+v, want the constraint-derived and the hazard-derived criteria and not the factory-drafted one", removed)
	}
	want := map[string]string{
		introduced[0].ID: score.ProvenanceConstraintDerived,
		introduced[1].ID: score.ProvenanceHazardDerived,
	}
	for _, one := range removed {
		provenance, expected := want[one.SubjectID]
		if !expected || one.Provenance != provenance {
			t.Errorf("%+v is not one of the two withdrawals with an authority", one)
		}
		if one.What != score.RemovedCriterion {
			t.Errorf("the removal reads %q, want a criterion withdrawn", one.What)
		}
		// Neither provenance names a human this composition can resolve — the
		// holder of a duty over the constraint or the area is a narrowing the
		// People declaration does not carry — so both route to the row's duty.
		if one.RoutedTo != "" {
			t.Errorf("%+v routes to a named human, and neither provenance names one here", one)
		}
	}
}
