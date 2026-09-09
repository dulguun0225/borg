// [gate.Gate.Fire]'s nothing-pending check takes one exception, the open event
// [gate.Gate.EditInPlace] appends naming the row it supersedes. A referred
// row's re-firing is not a second exception: by the time it fires, the row it
// was referred from is already closed, so the check passes on its own terms.
package gate_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/score"
)

// TestAReferredRowIsRefusedWherePriorAppendsLeaveAnotherRowOfItsOwnPending: a
// refer's re-firing goes through the same pending check every other firing
// does. It is refused where a second open event of the same row and subject
// somehow already stands pending, which is the one thing the design's own
// bound to a single exception (Edit in place) protects against.
func TestAReferredRowIsRefusedWherePriorAppendsLeaveAnotherRowOfItsOwnPending(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, token, g := newGate(t, s, p)

	declares(t, ctx, pool, token, owner, author.Key, gate.DutyUAT)
	declares(t, ctx, pool, token, owner, second.Key, gate.DutyUAT)

	merging := mergeRowFiring(t, ctx, pool, token)
	opened, err := g.Fire(ctx, merging)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}

	// A phantom second open event over the same row and the same item, written
	// directly rather than through the gate: nothing but [gate.Gate.EditInPlace]
	// may leave two of a row's own pending at once, and this stands in for
	// whatever would.
	payload, err := json.Marshal(gate.OpeningPayload{
		OpenEvent: score.OpenEvent{ItemID: merging.ItemID, Gate: gate.MergeToMaster.String()},
	})
	if err != nil {
		t.Fatalf("marshalling the phantom row's payload: %v", err)
	}
	phantom := decisionlog.NewWriter(pool, token)
	if _, err := phantom.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor:         gate.Component(gate.MergeToMaster),
		Payload:       string(payload),
		FormatVersion: "decision/1",
		PolicyVersion: testPolicyVersion,
		ScoreVersion:  testScoreVersion,
	}); err != nil {
		t.Fatalf("appending the phantom pending row: %v", err)
	}

	if _, err := g.Refer(ctx, opened, author, "I cannot judge this myself", merging); !errors.Is(err, gate.ErrRowPending) {
		t.Errorf("a refer whose re-firing finds another pending row of its own = %v, want ErrRowPending", err)
	}
}
