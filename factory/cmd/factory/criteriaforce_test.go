package main

import (
	"encoding/json"
	"testing"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/record"
)

func TestRejectedSpecWithdrawalDoesNotReachRevisedItem(t *testing.T) {
	ctx, d, _ := newPath(t, "")
	p := &path{d: d}
	var original criterion.Criterion
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	actor := record.Actor{Kind: record.KindComponent, Key: "artifact.store", Basis: record.BasisClaimed}
	original, err = criterion.Insert(ctx, tx, actor,
		criterion.Of{ServiceID: "svc_a", SpecArtifactID: "art_original", ItemID: "it_a"},
		criterion.Draft{Sentence: "The system shall retain the promise.", RequirementID: "rq_a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := criterion.Withdraw(ctx, tx, actor,
		criterion.Of{ServiceID: "svc_a", SpecArtifactID: "art_rejected", ItemID: "it_a"}, original.ID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	openingPayload := gate.OpeningPayload{}
	openingPayload.Gate = gate.Spec.String()
	openingPayload.ArtifactID = "art_rejected"
	payload, err := json.Marshal(openingPayload)
	if err != nil {
		t.Fatal(err)
	}
	log := decisionlog.NewWriter(d.pool, d.token)
	gateActor := record.Actor{Kind: record.KindComponent, Key: "gate.spec", Basis: record.BasisClaimed}
	opening, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{Actor: gateActor, Payload: string(payload), FormatVersion: "decision/1", PolicyVersion: "pv_test", ScoreVersion: "sv_test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := log.AppendDecisionClose(ctx, decisionlog.Entry{Actor: gateActor, Payload: "{}", FormatVersion: "decision/1", Closes: opening.ID, Verdict: string(gate.VerdictReject), Reason: "the withdrawal supersedes it"}); err != nil {
		t.Fatal(err)
	}
	active, err := p.criteriaInForce(ctx, "svc_a", []string{"it_a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != original.ID {
		t.Fatalf("active criteria = %v, want %s", active, original.ID)
	}
}
