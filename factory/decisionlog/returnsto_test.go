package decisionlog_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
)

// TestACloseNamesWhatAReturnsTo is C0847: a close event whose verdict sends the
// item back names the stage it returns to, empty where it does not.
func TestACloseNamesWhatAReturnsTo(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)

	opening, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "decision/1", PolicyVersion: "policy-1", ScoreVersion: "score-1",
	})
	if err != nil {
		t.Fatalf("AppendDecisionOpen: %v", err)
	}
	closing, err := log.AppendDecisionClose(ctx, decisionlog.Entry{
		Actor: owner, Payload: "x", FormatVersion: "decision/1", Verdict: "reject",
		Reason: "no good", Closes: opening.ID, ReturnsTo: "implementation",
	})
	if err != nil {
		t.Fatalf("AppendDecisionClose(reject, returns to implementation): %v", err)
	}
	if closing.ReturnsTo != "implementation" {
		t.Errorf("the closing returns to %q, want %q", closing.ReturnsTo, "implementation")
	}

	second, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "decision/1", PolicyVersion: "policy-1", ScoreVersion: "score-1",
	})
	if err != nil {
		t.Fatalf("AppendDecisionOpen: %v", err)
	}
	// A reject naming no stage is admitted too: decomposition's reject sends
	// the item nowhere, and the field stays unwritten.
	noTarget, err := log.AppendDecisionClose(ctx, decisionlog.Entry{
		Actor: owner, Payload: "x", FormatVersion: "decision/1", Verdict: "reject",
		Reason: "start over", Closes: second.ID,
	})
	if err != nil {
		t.Fatalf("AppendDecisionClose(reject, no target): %v", err)
	}
	if noTarget.ReturnsTo != "" {
		t.Errorf("a reject naming no target stored %q", noTarget.ReturnsTo)
	}

	if _, err := log.AppendDecisionClose(ctx, decisionlog.Entry{
		Actor: owner, Payload: "x", FormatVersion: "decision/1", Verdict: "approve",
		Closes: second.ID, ReturnsTo: "implementation",
	}); !errors.Is(err, decisionlog.ErrReturnsToRefused) {
		t.Errorf("an approve naming a target: %v, want ErrReturnsToRefused", err)
	}

	abandonmentOpening, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "decision/1", PolicyVersion: "policy-1", ScoreVersion: "score-1",
	})
	if err != nil {
		t.Fatalf("AppendDecisionOpen: %v", err)
	}
	if _, err := log.AppendDecisionAbandonment(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "decision/1", Closes: abandonmentOpening.ID,
		Reason: "dropped", ReturnsTo: "implementation",
	}); !errors.Is(err, decisionlog.ErrReturnsToRefused) {
		t.Errorf("an abandonment naming a target: %v, want ErrReturnsToRefused", err)
	}

	badRow := aRow()
	badRow.FormatVersion, badRow.Shape, badRow.Part = "decision/1", decisionlog.ShapeDecision, decisionlog.PartClose
	badRow.Closes, badRow.Verdict, badRow.ReturnsTo = second.ID, "approve", "implementation"
	if got, want := refusedBy(t, insertAround(ctx, pool, badRow)), "returns_to_scope"; got != want {
		t.Errorf("an approve naming a target around the method was refused by %q, want %q", got, want)
	}

	if err := reader.Verify(ctx, ownerReading); err != nil {
		t.Fatalf("a refused row reached the log: %v", err)
	}
}

// TestAReworkRequestNamesTheDefectAndWhereItReturnsTo is C1016: the defect
// stated is the request's reason, the field a reject's close event carries,
// and the stage it names is handed it the same way.
func TestAReworkRequestNamesTheDefectAndWhereItReturnsTo(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)

	row, err := log.AppendReworkRequest(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "rework_request/1",
		Reason: "the spec says two things", ReturnsTo: "spec",
	})
	if err != nil {
		t.Fatalf("AppendReworkRequest: %v", err)
	}
	if row.Reason != "the spec says two things" || row.ReturnsTo != "spec" {
		t.Errorf("the rework request is %+v, want the reason and the stage", row)
	}

	if _, err := log.AppendReworkRequest(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "rework_request/1", ReturnsTo: "spec",
	}); !errors.Is(err, decisionlog.ErrReasonMissing) {
		t.Errorf("a rework request with no reason: %v, want ErrReasonMissing", err)
	}
	if _, err := log.AppendReworkRequest(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "rework_request/1", Reason: "the spec says two things",
	}); !errors.Is(err, decisionlog.ErrReturnsToMissing) {
		t.Errorf("a rework request naming no stage: %v, want ErrReturnsToMissing", err)
	}

	if err := reader.Verify(ctx, ownerReading); err != nil {
		t.Fatalf("a refused row reached the log: %v", err)
	}
}
