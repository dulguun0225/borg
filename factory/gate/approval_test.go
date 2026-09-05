// ApprovalTimes, which orders the merge queue: the latest approval per item at
// one row, a rejected item named at none, and a row this package cannot read
// skipped rather than returned as an error.
package gate_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/record"
)

// TestApprovalTimesIsWhatOrdersTheMergeQueue: the queue's order is the item's
// priority and then the time of the merge approval in the log, and that time is a
// fact of no record — so this package, which owns the shape of both payloads,
// answers it. A rejected item has no approval, and an item approved twice keeps the
// latest.
func TestApprovalTimesIsWhatOrdersTheMergeQueue(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, token, g := newGate(t, s, p)
	human := owner

	fire := func(row gate.Row, itemID string, verdict gate.Verdict, feedback string) string {
		t.Helper()
		firing := mergeFiring
		firing.Row = row
		firing.ItemID = itemID
		if row != gate.MergeToMaster {
			firing.ArtifactID = ""
		}
		opened, err := g.Fire(ctx, firing)
		if err != nil {
			t.Fatalf("Fire at %s for %s: %v", row, itemID, err)
		}
		closing, err := g.Decide(ctx, opened, gate.Given{Actor: human, Verdict: verdict, Reason: feedback})
		if err != nil {
			t.Fatalf("Decide at %s for %s: %v", row, itemID, err)
		}
		return closing.At
	}

	const approvedItem, rejectedItem = "it_approved", "it_rejected"
	fire(gate.MergeToMaster, approvedItem, gate.VerdictApprove, "")
	fire(gate.MergeToMaster, rejectedItem, gate.VerdictReject, "not this one")
	// A decision at another row is not this row's, however it closed.
	fire(gate.DeployToCandidateEnvironment, rejectedItem, gate.VerdictApprove, "")
	// Approved again: the queue's order is about the approval in force.
	latest := fire(gate.MergeToMaster, approvedItem, gate.VerdictApprove, "")

	// A row in a shape this package cannot read is skipped rather than returned as
	// an error, the way every other reader of this log treats one.
	if _, err := decisionlog.NewWriter(pool, token).AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor:         record.Actor{Kind: record.KindComponent, Key: "gate.some_other_gate"},
		Payload:       "a payload this package has no shape for",
		FormatVersion: "decision/1",
		PolicyVersion: testPolicyVersion,
		ScoreVersion:  testScoreVersion,
	}); err != nil {
		t.Fatalf("appending the unreadable open event: %v", err)
	}

	times, err := gate.ApprovalTimes(ctx, pool, token, owner, gate.MergeToMaster)
	if err != nil {
		t.Fatalf("ApprovalTimes: %v", err)
	}
	if len(times) != 1 {
		t.Fatalf("ApprovalTimes = %+v, want the one item approved at that row", times)
	}
	if times[approvedItem] != latest {
		t.Errorf("the approval of %s reads as %q, want the latest one at %q", approvedItem, times[approvedItem], latest)
	}
	if _, ours := times[rejectedItem]; ours {
		t.Errorf("ApprovalTimes names %s, which was rejected at that row", rejectedItem)
	}

	if _, err := gate.ApprovalTimes(ctx, pool, token, owner, gate.Of(gate.Kind("some_other_row"))); !errors.Is(err, gate.ErrRowUnknown) {
		t.Errorf("ApprovalTimes at a row this package does not fire = %v, want ErrRowUnknown", err)
	}
}
