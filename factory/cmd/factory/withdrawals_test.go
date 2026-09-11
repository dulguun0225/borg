// What a spec version under decision removes, read on an install where no spec
// version was ever decided by a human — the steady state the design describes,
// the score auto-passing the Spec row.
package main

import (
	"encoding/json"
	"testing"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/record"
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
		}, nil, nil, "im_1")
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
		"it_withdrawals", svc.ID, "the spec that withdraws them", nil, withdrawn, nil, "im_1")
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

// TestAHumanConfirmedWithdrawalRoutesToAnotherHolderOfTheDutyWhereTheDeciderNoLongerHolds:
// a human-confirmed criterion's withdrawal names the actor of the decision that
// introduced it, and where that actor no longer holds duty 6 it names another
// holder instead — never nobody, which the score would otherwise send to the
// owner by default.
func TestAHumanConfirmedWithdrawalRoutesToAnotherHolderOfTheDutyWhereTheDeciderNoLongerHolds(t *testing.T) {
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
		"it_confirmed", svc.ID, "the spec that introduces the criterion", []criterion.Draft{
			{
				Sentence:      "When a report is asked for, the system shall answer it.",
				RequirementID: "rq_drafted",
			},
		}, nil, nil, "im_1")
	if err != nil {
		t.Fatalf("submitting the spec version that introduces it: %v", err)
	}
	if len(introduced) != 1 {
		t.Fatalf("the version introduced %d criteria, want one", len(introduced))
	}

	// The decider is a human who does not hold duty 6, and the duty's only
	// holder is somebody else.
	decider := record.Actor{Kind: record.KindHuman, Key: "hum_decider", Basis: record.BasisClaimed}
	otherHolder := record.Actor{Kind: record.KindHuman, Key: "hum_other_holder", Basis: record.BasisClaimed}
	if _, err := people.NewWriter(d.pool, d.token, nil).Declare(ctx, otherHolder, otherHolder.Key,
		people.OfDuty(gate.DutyConfirmTheCriteria)); err != nil {
		t.Fatalf("declaring the other holder of duty 6: %v", err)
	}

	payload, err := json.Marshal(gate.OpeningPayload{OpenEvent: score.OpenEvent{
		ItemID: "it_confirmed", ArtifactID: introducing.ID, Gate: gate.Spec.String(),
	}})
	if err != nil {
		t.Fatalf("marshalling the opening payload: %v", err)
	}
	w := decisionlog.NewWriter(d.pool, d.token)
	opened, err := w.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor: gate.Component(gate.Spec), Payload: string(payload),
		FormatVersion: "decision/1", PolicyVersion: "pv_test", ScoreVersion: "sv_test",
	})
	if err != nil {
		t.Fatalf("appending the Spec row's opening: %v", err)
	}
	if _, err := w.AppendDecisionClose(ctx, decisionlog.Entry{
		Actor: decider, FormatVersion: "decision/1", Verdict: "approve", Closes: opened.ID,
	}); err != nil {
		t.Fatalf("appending the decider's approval: %v", err)
	}

	withdrawing, _, _, err := p.store.SubmitSpec(ctx, p.specAuthorActor(), by,
		"it_confirmed", svc.ID, "the spec that withdraws it", nil,
		[]string{introduced[0].ID}, nil, "im_1")
	if err != nil {
		t.Fatalf("submitting the withdrawing spec version: %v", err)
	}

	removed, err := (withdrawals{pool: d.pool, token: d.token}).ProtectionRemovedBy(ctx, withdrawing.ID)
	if err != nil {
		t.Fatalf("reading what the version removes: %v", err)
	}
	if len(removed) != 1 {
		t.Fatalf("the version removes %+v, want the one human-confirmed criterion", removed)
	}
	if removed[0].Provenance != score.ProvenanceHumanConfirmed {
		t.Errorf("the withdrawal's provenance is %q, want human-confirmed", removed[0].Provenance)
	}
	if removed[0].RoutedTo != otherHolder.Key {
		t.Errorf("the withdrawal routes to %q, want the other holder %q, the decider no longer holding duty 6",
			removed[0].RoutedTo, otherHolder.Key)
	}
}
