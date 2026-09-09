// The per-author prior after a restart: [score.Score.prior] counts only what
// closed after the time it restarted at.
//
// It is in score_test for the reason report_test.go is: the fixture applies
// the whole factory schema through package postgres, which reaches this
// package back, so an internal test file importing postgres would make
// package score import itself. It does not skip when the database is
// unreachable.
package score_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
)

// TestARestartedPriorIgnoresWhatClosedBeforeIt: once an author's prior has
// restarted, an approval that closed before the restart does not narrow it —
// only what closed after the restart time counts, the way an unseen author's
// count of closes starts at none.
func TestARestartedPriorIgnoresWhatClosedBeforeIt(t *testing.T) {
	ctx, pool, log, _, _, token := newReportStore(t)
	human := record.Actor{Kind: record.KindHuman, Key: "person:reviewer", Basis: record.BasisClaimed}

	store := artifact.NewStore(pool, token)
	older, err := store.SubmitImplementation(ctx, marking,
		artifact.By{Authorship: artifact.AuthorshipAgent, Author: "model-1"}, "it_older", "older content", "im_older")
	if err != nil {
		t.Fatalf("submitting the older version: %v", err)
	}
	newer, err := store.SubmitImplementation(ctx, marking,
		artifact.By{Authorship: artifact.AuthorshipAgent, Author: "model-1"}, "it_newer", "newer content", "im_newer")
	if err != nil {
		t.Fatalf("submitting the newer version: %v", err)
	}

	// The older decision approves before the restart, and would narrow the
	// prior toward good if it still counted.
	appendFiring(t, ctx, log, gateComponent("implementation"), human,
		score.OpenEvent{ItemID: "it_older", ArtifactID: older.ID, Gate: "implementation"},
		score.CloseEvent{Verdict: score.VerdictApproved})

	restartedAt := record.Now()

	// The newer decision rejects after the restart, and is the only one a
	// restarted prior reads.
	appendRejection(t, ctx, log, gateComponent("implementation"), human,
		score.OpenEvent{ItemID: "it_newer", ArtifactID: newer.ID, Gate: "implementation"},
		score.CloseEvent{Verdict: score.VerdictRejected})

	change := score.Change{
		ItemID: "it_newer", ServiceID: "svc_1", ArtifactID: newer.ID, FactorSet: score.SetAboveABuild,
	}

	before := score.New(score.Composition{Pool: pool, Draw: score.NeverDraw{}, Token: token})
	unrestarted, err := before.Assess(ctx, change)
	if err != nil {
		t.Fatalf("Assess before a restart: %v", err)
	}
	priorBefore, found := factorNamed(unrestarted, "author.prior")
	if !found {
		t.Fatalf("the vector carries no author prior: %+v", unrestarted.Vector)
	}
	if !strings.Contains(priorBefore.Reading, "1 human approval") || !strings.Contains(priorBefore.Reading, "1 rejection") {
		t.Errorf("with no restart the prior reads %q, want both the older approval and the newer rejection counted", priorBefore.Reading)
	}

	restarted := score.New(score.Composition{
		Pool: pool, Draw: score.NeverDraw{}, Token: token,
		Version: score.Version{PriorRestarts: map[string]string{"model-1": restartedAt}},
	})
	after, err := restarted.Assess(ctx, change)
	if err != nil {
		t.Fatalf("Assess after a restart: %v", err)
	}
	priorAfter, found := factorNamed(after, "author.prior")
	if !found {
		t.Fatalf("the vector carries no author prior: %+v", after.Vector)
	}
	if !strings.Contains(priorAfter.Reading, "0 human approval") {
		t.Errorf("a restarted prior reads %q, want the older approval that closed before the restart ignored", priorAfter.Reading)
	}
	if !strings.Contains(priorAfter.Reading, "1 rejection") {
		t.Errorf("a restarted prior reads %q, want the newer rejection that closed after the restart counted", priorAfter.Reading)
	}
}

// appendRejection is [appendFiring] for a reject: the one verdict that names
// a reason, which report_test.go's own helper never writes because none of
// its callers reject.
func appendRejection(t *testing.T, ctx context.Context, log *decisionlog.Writer,
	openActor, closeActor record.Actor, open score.OpenEvent, closeEvent score.CloseEvent) {
	t.Helper()

	openPayload, err := json.Marshal(open)
	if err != nil {
		t.Fatalf("encoding the open event: %v", err)
	}
	opened, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor: openActor, Payload: string(openPayload), FormatVersion: "decision/1",
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
		Actor: closeActor, Payload: string(closePayload), FormatVersion: "decision/1",
		Closes: opened.ID, Verdict: closeEvent.Verdict, Reason: "the change was wrong",
		OpenedInWorkAt: record.Now(),
	}); err != nil {
		t.Fatalf("AppendDecisionClose: %v", err)
	}
}
