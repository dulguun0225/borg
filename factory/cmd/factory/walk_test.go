// The walk skipping a decision row whose payload it cannot read.
package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
)

// TestTheWalkSkipsAPayloadItCannotRead appends an open event whose payload is
// not the gate's shape, before the run, so the walk meets it first. A payload
// is unconstrained bytes by decisionlog's contract, so a row the walk cannot
// read is skipped and the search goes on — one such row does not take down
// every walk.
func TestTheWalkSkipsAPayloadItCannotRead(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)

	_, err := decisionlog.NewWriter(d.pool, d.token).AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor:         gate.Component(gate.Of("some_other_gate")),
		Payload:       "a payload this walk has no shape for",
		FormatVersion: "decision/1",
		PolicyVersion: "policy-unauthored-m1",
		ScoreVersion:  "score-stub-m1",
	})
	if err != nil {
		t.Fatalf("appending the unreadable open event: %v", err)
	}

	res, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	c := only(t, res)

	var walked bytes.Buffer
	if err := walk(ctx, d.pool, &walked, d.token, asPrincipal(owner(t, ctx, d.pool, d.token, d.human)), c.deployID); err != nil {
		t.Fatalf("the walk stopped on a row it cannot read: %v\noutput so far:\n%s", err, walked.String())
	}
	if !strings.Contains(walked.String(), theStatement) {
		t.Errorf("the walk from %s does not reach the statement %q:\n%s", c.deployID, theStatement, walked.String())
	}
}
