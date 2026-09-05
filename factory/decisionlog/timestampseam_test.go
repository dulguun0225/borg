package decisionlog_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
)

// TestTheStoreRefusesATimestampThatIsNotTheLayout is what the record.Columns
// timestamp is worth to a package that is not this one. This writer always
// uses record.Now, so the constraint says nothing about it; what it says is
// that the next package to compose record.Columns cannot quietly store a
// second format. The chain would hash and verify whatever bytes were there,
// so the store is the only thing that can refuse them.
//
// The accepting case needs no assertion here: every other database test in
// this package writes record.Now through the writer, so the constraint
// refusing what the writer produces would fail all of them.
func TestTheStoreRefusesATimestampThatIsNotTheLayout(t *testing.T) {
	ctx, pool, _, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)
	for _, at := range []string{
		"",
		"2026-08-17T01:30:00Z",
		"2026-08-17T01:30:00.000Z",
		"2026-08-17T01:30:00.000000000+00:00",
		"2026-08-17T01:30:00.000000000Z ",
		"not a time at all",
	} {
		row := aRow()
		row.At = at
		if got, want := refusedBy(t, insertAround(ctx, pool, row)), "at_is_time_layout"; got != want {
			t.Errorf("the timestamp %q was refused by %q, want %q", at, got, want)
		}
	}
	if err := reader.Verify(ctx, owner); err != nil {
		t.Fatalf("a refused row reached the log: %v", err)
	}
}

// TestOpenedInWorkAtMustBeEmptyOrTheLayout is the same constraint over the
// close event's own column: empty is allowed, and anything else has to be
// record.TimeLayout.
func TestOpenedInWorkAtMustBeEmptyOrTheLayout(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)

	opening, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "decision/1", PolicyVersion: "policy-1", ScoreVersion: "score-1",
	})
	if err != nil {
		t.Fatalf("AppendDecisionOpen: %v", err)
	}
	if _, err := log.AppendDecisionClose(ctx, decisionlog.Entry{
		Actor: owner, FormatVersion: "decision/1", Verdict: "approve", Closes: opening.ID,
		OpenedInWorkAt: "not a time at all",
	}); err == nil {
		t.Fatal("a malformed OpenedInWorkAt was accepted")
	}

	if err := reader.Verify(ctx, owner); err != nil {
		t.Fatalf("a refused row reached the log: %v", err)
	}
}
