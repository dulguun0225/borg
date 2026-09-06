// Tests of the encoding check's defect rejecting at the merge row on its own
// terms, before a verdict is asked for — the mechanical rejection
// [gate.AutoRejectedByEncoding] names, which nothing called until this
// milestone — and of a could-not-derive putting a human at the row instead of
// rejecting, the way [gate.CouldNotDeriveEncoding] already did for nothing.
package main

import (
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/item"
)

// TestAnEncodingDefectIsRejectedAtTheMergeRowBeforeAVerdict is the defect this
// milestone fixes: a candidate whose build's encodings do not match the
// criteria in force is rejected at the merge row on the defect's own terms,
// never stopping the run outright the way [path.checkEncodings] used to.
func TestAnEncodingDefectIsRejectedAtTheMergeRowBeforeAVerdict(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	if _, err := run(ctx, d, of(theStatement)); err != nil {
		t.Fatalf("the first run stopped: %v\noutput so far:\n%s", err, out)
	}

	d.in = strings.NewReader(approvals)
	path := p(ctx, t, d)
	c := authorOne(t, ctx, path, theSecondStatement, out)
	if err := path.candidateEnvironment(ctx, c); err != nil {
		t.Fatalf("the candidate environment: %v\noutput so far:\n%s", err, out)
	}
	if c.encodingDefect != "" || c.encodingCouldNotDerive {
		t.Fatalf("the candidate already carries an encoding defect before this test set one: %q, could-not-derive %v",
			c.encodingDefect, c.encodingCouldNotDerive)
	}
	// The candidate's own run is made to carry an encoding defect, the way
	// [path.checkEncodings] would have set it — the exact field the merge row
	// reads to decide this, one line of the joined check errors.
	c.encodingDefect = "criterion: cr_deadbeefdeadbeefdeadbeefdeadbeef is in force and no encoding in the build names it"

	if err := path.mergeGate(ctx, c); err != nil {
		t.Fatalf("the Merge to master gate: %v\noutput so far:\n%s", err, out)
	}

	if !c.autoRejected || c.autoRejectedBy != gate.AutoRejectedByEncoding {
		t.Fatalf("the candidate was rejected by %q (auto %v), want %q",
			c.autoRejectedBy, c.autoRejected, gate.AutoRejectedByEncoding)
	}
	if c.queued || c.merged {
		t.Fatalf("a candidate rejected by an encoding defect reached the queue: queued %v, merged %v", c.queued, c.merged)
	}
	members, err := path.queue.Members(ctx, theServiceRecord(t, ctx, path).ID)
	if err != nil {
		t.Fatalf("reading the queue's members: %v", err)
	}
	for _, m := range members {
		if m.ID == c.itemID {
			t.Fatalf("the rejected item %s is a member of the merge queue", c.itemID)
		}
	}

	it, err := item.Get(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}
	if it.Stage != item.StageImplementation {
		t.Errorf("the rejected item is at %s, want implementation", it.Stage)
	}
	if !strings.Contains(out.String(), "before a verdict was asked for") {
		t.Errorf("the run does not say the check rejected before a verdict:\n%s", out)
	}
	if !strings.Contains(out.String(), "an attempt counted there") {
		t.Errorf("the run does not say the item goes back with an attempt counted:\n%s", out)
	}

	closing := closingOf(t, ctx, d, c.mergeGate.closing)
	if closing.AutoRejectedBy != gate.AutoRejectedByEncoding {
		t.Errorf("the close event's auto_rejected_by is %q, want %q", closing.AutoRejectedBy, gate.AutoRejectedByEncoding)
	}
	if closing.Reason != c.encodingDefect {
		t.Errorf("the close event's reason is %q, want the encoding defect %q", closing.Reason, c.encodingDefect)
	}
}

// TestAnEncodingCouldNotDerivePutsAHumanAtTheMergeRow is the other outcome
// [path.checkEncodings] carries on the candidate: a derivation that could not
// be made is not a rejection, and it puts a human at the row the way a
// security predicate's own could-not-derive already does.
func TestAnEncodingCouldNotDerivePutsAHumanAtTheMergeRow(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	if _, err := run(ctx, d, of(theStatement)); err != nil {
		t.Fatalf("the first run stopped: %v\noutput so far:\n%s", err, out)
	}

	d.in = strings.NewReader(approvals)
	path := p(ctx, t, d)
	c := authorOne(t, ctx, path, theSecondStatement, out)
	if err := path.candidateEnvironment(ctx, c); err != nil {
		t.Fatalf("the candidate environment: %v\noutput so far:\n%s", err, out)
	}
	// The same field [path.checkEncodings] sets where the encodings could not be
	// derived at all — no extractor covering the toolchain — rather than a
	// defect over what one found.
	c.encodingCouldNotDerive = true

	if err := path.mergeGate(ctx, c); err != nil {
		t.Fatalf("the Merge to master gate: %v\noutput so far:\n%s", err, out)
	}

	if c.autoRejected {
		t.Fatalf("a could-not-derive rejected the row rather than asking a human: autoRejectedBy %q", c.autoRejectedBy)
	}
	if !c.mergeGate.humanDecided {
		t.Fatal("an encoding could-not-derive did not put a human at the merge row")
	}
	opened := openingOf(t, ctx, d, c.mergeGate.opening)
	found := false
	for _, d := range opened.CouldNotDerive {
		if d == gate.CouldNotDeriveEncoding {
			found = true
		}
	}
	if !found {
		t.Errorf("the open event's could_not_derive is %v, want it to carry %q", opened.CouldNotDerive, gate.CouldNotDeriveEncoding)
	}
	// The scripted input approves the row a human was put at, so the candidate
	// still reaches the queue — the could-not-derive asked for a verdict and
	// got one, rather than rejecting on its own terms.
	if !c.queued {
		t.Fatal("the candidate did not reach the merge queue after a human approved the could-not-derive row")
	}
}
