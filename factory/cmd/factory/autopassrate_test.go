// The wiring [newFactory] and [autoPassRates] give package policy: a
// threshold write reads the score's own realized auto-pass rate through
// [score.RealizedAutoPass], one row per factor set, and freezes it on the
// version. policy's own autopassrate_test.go demonstrates the freezing with a
// fake; this is the composition that reads the real one.
package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
)

// appendClosedFiring writes one decision open and its close, the JSON
// payloads being exactly what package score's own reader reads back — the
// arrangement score/report_test.go's appendFiring already demonstrates.
func appendClosedFiring(t *testing.T, ctx context.Context, log *decisionlog.Writer,
	open score.OpenEvent, closeEvent score.CloseEvent) {
	t.Helper()
	gate := record.Actor{Kind: record.KindComponent, Key: "gate." + open.Gate, Basis: record.BasisClaimed}

	openPayload, err := json.Marshal(open)
	if err != nil {
		t.Fatalf("encoding the open event: %v", err)
	}
	opened, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: string(openPayload), FormatVersion: "decision/1",
		PolicyVersion: "pv_1", ScoreVersion: "sv_1",
	})
	if err != nil {
		t.Fatalf("AppendDecisionOpen: %v", err)
	}
	closePayload, err := json.Marshal(closeEvent)
	if err != nil {
		t.Fatalf("encoding the close event: %v", err)
	}
	if _, err := log.AppendDecisionClose(ctx, decisionlog.Entry{
		Actor: gate, Payload: string(closePayload), FormatVersion: "decision/1",
		Closes: opened.ID, Verdict: closeEvent.Verdict, OpenedInWorkAt: record.Now(),
	}); err != nil {
		t.Fatalf("AppendDecisionClose: %v", err)
	}
}

// TestAThresholdVersionCarriesARatePerFactorSet is the wiring end to end:
// AuthorGateThreshold reads the realized auto-pass rate at the row and the
// number it authors from every closed decision the log already holds, one row
// per factor set, and freezes it on the version it appends.
func TestAThresholdVersionCarriesARatePerFactorSet(t *testing.T) {
	ctx, pool := newOwner(t)
	prod := install(t, ctx, pool)
	token := testToken(t, ctx, pool)
	log := decisionlog.NewWriter(pool, token)

	const row, threshold = "merge_to_master", 0.3
	// Two decisions at one factor set, one auto-passed by the threshold and one
	// a human approved on their own reading: half the row's decisions auto-passed.
	appendClosedFiring(t, ctx, log,
		score.OpenEvent{ItemID: "it_a", Gate: row, FactorSet: score.SetWithABuild, Number: 0.1, Threshold: threshold},
		score.CloseEvent{Verdict: score.VerdictApproved, WhyItAutoPassed: score.AutoPassThreshold})
	appendClosedFiring(t, ctx, log,
		score.OpenEvent{ItemID: "it_b", Gate: row, FactorSet: score.SetWithABuild, Number: 0.5, Threshold: threshold},
		score.CloseEvent{Verdict: score.VerdictApproved})
	// One decision at the other factor set, auto-passed: all of that row's
	// decisions auto-passed.
	appendClosedFiring(t, ctx, log,
		score.OpenEvent{ItemID: "it_c", Gate: row, FactorSet: score.SetAboveABuild, Number: 0.1, Threshold: threshold},
		score.CloseEvent{Verdict: score.VerdictApproved, WhyItAutoPassed: score.AutoPassThreshold})

	factory := newFactory(pool, token)
	version, err := factory.AuthorGateThreshold(ctx, owner(t, ctx, pool, token, "owner"), prod.ID, row, threshold)
	if err != nil {
		t.Fatalf("AuthorGateThreshold: %v", err)
	}
	if len(version.AutoPassRates) != 2 {
		t.Fatalf("the version froze %d rate(s), want one per factor set: %+v", len(version.AutoPassRates), version.AutoPassRates)
	}
	rates := map[string]float64{}
	for _, r := range version.AutoPassRates {
		rates[r.FactorSet] = r.Rate
	}
	if got := rates[string(score.SetWithABuild)]; got != 0.5 {
		t.Errorf("the rate for %q = %v, want 0.5", score.SetWithABuild, got)
	}
	if got := rates[string(score.SetAboveABuild)]; got != 1 {
		t.Errorf("the rate for %q = %v, want 1", score.SetAboveABuild, got)
	}
}
