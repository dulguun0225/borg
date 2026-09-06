package policy_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
)

// TestThePolicyVersionFieldsTheScoreReads holds the two spellings together. The
// score reads two fields off a policy version row — the scope and the score
// version the write confirmed — and cannot import this package, which imports
// it. What it reads is the JSON below.
func TestThePolicyVersionFieldsTheScoreReads(t *testing.T) {
	ctx, in := newFactory(t)

	version, err := in.factory.ConfirmGateThreshold(ctx, owner, in.prod.ID, "merge_to_master")
	if err != nil {
		t.Fatalf("ConfirmGateThreshold: %v", err)
	}
	rows, err := in.reader.Versions(ctx, ownerReading)
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if got := rows[len(rows)-1].ConfirmsScoreVersion; got != version.ConfirmsScoreVersion || got == "" {
		t.Fatalf("the version read back confirms %q, want %q", got, version.ConfirmsScoreVersion)
	}

	var payload struct {
		Scope struct {
			Kind string `json:"kind"`
			ID   string `json:"id"`
			Key  string `json:"key"`
		} `json:"scope"`
		ConfirmsScoreVersion string `json:"confirms_score_version"`
	}
	body := payloadOfNewestVersion(t, ctx, in)
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("reading the row the score reads: %v", err)
	}
	if payload.Scope.Kind != policy.ScopeEnvironment || payload.Scope.ID != in.prod.ID ||
		payload.Scope.Key != "merge_to_master" {
		t.Errorf("the scope reads back as %+v", payload.Scope)
	}
	if payload.ConfirmsScoreVersion != version.ConfirmsScoreVersion {
		t.Errorf("confirms_score_version reads back as %q, want %q",
			payload.ConfirmsScoreVersion, version.ConfirmsScoreVersion)
	}
	// The string package score composes the confirmation's key from.
	if got, want := payload.Scope.Kind+":"+payload.Scope.ID+":"+payload.Scope.Key,
		(policy.Scope{Kind: policy.ScopeEnvironment, ID: in.prod.ID, Key: "merge_to_master"}).String(); got != want {
		t.Errorf("the scope keys as %q here and %q there", want, got)
	}
}

// TestAThresholdWriteNamesTheScoreVersionItWasAuthoredAgainst: what an owner
// authors is compared against the number the published formula returns, so the
// write names the version that returns it, and confirming the same one twice
// appends nothing.
func TestAThresholdWriteNamesTheScoreVersionItWasAuthoredAgainst(t *testing.T) {
	ctx, in := newFactory(t)

	inForce, found, err := score.Newest(ctx, in.pool, in.token)
	if err != nil || !found {
		t.Fatalf("the score version in force: %v", err)
	}

	authored, err := in.factory.AuthorGateThreshold(ctx, owner, in.prod.ID, "merge_to_master", 0.4)
	if err != nil {
		t.Fatalf("AuthorGateThreshold: %v", err)
	}
	if authored.ConfirmsScoreVersion != inForce.ID {
		t.Errorf("the threshold write confirms %q, want the version in force %q",
			authored.ConfirmsScoreVersion, inForce.ID)
	}

	confirmed, err := in.factory.ConfirmGateThreshold(ctx, owner, in.prod.ID, "merge_to_master")
	if err != nil {
		t.Fatalf("ConfirmGateThreshold: %v", err)
	}
	if confirmed.Action != policy.ActionConfirmed || confirmed.ConfirmsScoreVersion != inForce.ID {
		t.Errorf("the confirmation is %+v", confirmed)
	}
	again, err := in.factory.ConfirmGateThreshold(ctx, owner, in.prod.ID, "merge_to_master")
	if err != nil {
		t.Fatalf("ConfirmGateThreshold twice: %v", err)
	}
	if again.ID != confirmed.ID {
		t.Errorf("confirming the same score version twice appended %s beside %s", again.ID, confirmed.ID)
	}
}

// TestAFormulaChangeWaitsOnTheOwnerWhereAThresholdIsAuthored: a version that
// redefines the number is in force at once where nobody authored a threshold,
// and waits at a scope where somebody did until they confirm or re-author. What
// the gate reads is which version its own firing computes under.
func TestAFormulaChangeWaitsOnTheOwnerWhereAThresholdIsAuthored(t *testing.T) {
	ctx, in := newFactory(t)

	first, found, err := score.Newest(ctx, in.pool, in.token)
	if err != nil || !found {
		t.Fatalf("the score version in force: %v", err)
	}
	if _, err := in.factory.AuthorGateThreshold(ctx, owner, in.prod.ID, "merge_to_master", 0.4); err != nil {
		t.Fatalf("AuthorGateThreshold: %v", err)
	}

	// A version that changes the published formula or the factor set, which is
	// what an upgrade's first start appends: under a changed formula the same
	// change gets a different number.
	scorer := score.NewWriter(in.pool, in.token, score.NoMarks{})
	recalibrated, err := scorer.EnterShipped(ctx, record.Actor{Kind: record.KindComponent, Key: "score", Basis: record.BasisClaimed}, "borg/2.0.0")
	if err != nil {
		t.Fatalf("EnterShipped: %v", err)
	}
	if recalibrated.ID == first.ID || recalibrated.Branch == score.BranchSupplied {
		t.Fatalf("the version appended is %s, and nothing here waits on an owner", recalibrated.Branch)
	}

	reader := policy.NewReader(in.pool, in.token, recalibrated)
	authored, err := reader.AtGate(ctx, ownerReading, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("AtGate: %v", err)
	}
	if authored.ScoreVersion != first.ID {
		t.Errorf("the row an owner authored decides under %q, want the version confirmed there %q",
			authored.ScoreVersion, first.ID)
	}
	if authored.ScoreVersionWaiting != recalibrated.ID {
		t.Errorf("the row waits on %q, want %q", authored.ScoreVersionWaiting, recalibrated.ID)
	}

	unauthored, err := reader.AtGate(ctx, ownerReading, in.subjects("implementation"))
	if err != nil {
		t.Fatalf("AtGate: %v", err)
	}
	if unauthored.ScoreVersion != recalibrated.ID || unauthored.ScoreVersionWaiting != "" {
		t.Errorf("a row nobody authored a threshold at decides under %q and waits on %q, want the newest %q and nothing",
			unauthored.ScoreVersion, unauthored.ScoreVersionWaiting, recalibrated.ID)
	}

	// Confirming is what puts it in force there.
	if _, err := in.factory.ConfirmGateThreshold(ctx, owner, in.prod.ID, "merge_to_master"); err != nil {
		t.Fatalf("ConfirmGateThreshold: %v", err)
	}
	confirmed, err := reader.AtGate(ctx, ownerReading, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("AtGate: %v", err)
	}
	if confirmed.ScoreVersion != recalibrated.ID || confirmed.ScoreVersionWaiting != "" {
		t.Errorf("after the confirmation the row decides under %q and waits on %q, want %q and nothing",
			confirmed.ScoreVersion, confirmed.ScoreVersionWaiting, recalibrated.ID)
	}
	// The threshold itself is the owner's, unmoved by any of it.
	if confirmed.Threshold != 0.4 || confirmed.ThresholdFrom != policy.FromAuthored {
		t.Errorf("the threshold reads %v from %s, want the authored 0.4", confirmed.Threshold, confirmed.ThresholdFrom)
	}
	if _, found := score.Starting(gatepolicy.RiskThreshold); !found {
		t.Error("the score supplies no risk threshold")
	}
}

// payloadOfNewestVersion is the stored text of the newest policy version row,
// which is what package score unmarshals.
func payloadOfNewestVersion(t *testing.T, ctx context.Context, in installed) string {
	t.Helper()
	var payload string
	err := in.pool.QueryRow(ctx, `select payload from decision_log
		where shape = 'policy_version' order by seq desc limit 1`).Scan(&payload)
	if err != nil {
		t.Fatalf("reading the newest policy version row: %v", err)
	}
	if !strings.Contains(payload, "confirms_score_version") {
		t.Fatalf("the row the score reads holds no confirms_score_version: %s", payload)
	}
	return payload
}
