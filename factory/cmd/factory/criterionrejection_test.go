// Tests of the acceptance criteria rejecting at the merge row on their own
// terms, before a verdict is asked for — the mechanical rejection
// [gate.AutoRejectedByCriterion] names, which nothing called until this
// milestone.
package main

import (
	"context"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/score"
)

// TestAFailedCriterionIsRejectedAtTheMergeRowBeforeAVerdict is the defect this
// milestone fixes: a candidate whose run failed a criterion in force is
// rejected at the merge row on the criterion's own terms, never reaching the
// merge queue, rather than being auto-passed by the threshold and rejected
// only later, at the queue's own re-verification.
func TestAFailedCriterionIsRejectedAtTheMergeRowBeforeAVerdict(t *testing.T) {
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
	if len(c.criteria) == 0 {
		t.Fatal("the candidate environment decided no criteria")
	}
	for _, result := range c.criteria {
		if result.Outcome.Blocks(result.Unreliable) {
			t.Fatalf("criterion %s is already %s before this test failed one", result.CriterionID, result.Outcome)
		}
	}
	// The candidate's own run is made to fail one criterion in force, the way a
	// real run's encodings would have recorded it — the exact shape
	// [gate.CriterionResult] takes, and the only field the merge row reads to
	// decide this.
	failing := c.criteria[0].CriterionID
	c.criteria[0].Outcome = criterion.OutcomeFailed

	if err := path.mergeGate(ctx, c); err != nil {
		t.Fatalf("the Merge to master gate: %v\noutput so far:\n%s", err, out)
	}

	if !c.autoRejected || c.autoRejectedBy != gate.AutoRejectedByCriterion {
		t.Fatalf("the candidate was rejected by %q (auto %v), want %q",
			c.autoRejectedBy, c.autoRejected, gate.AutoRejectedByCriterion)
	}
	if c.queued || c.merged {
		t.Fatalf("a candidate rejected by a failed criterion reached the queue: queued %v, merged %v", c.queued, c.merged)
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
	if closing.AutoRejectedBy != gate.AutoRejectedByCriterion {
		t.Errorf("the close event's auto_rejected_by is %q, want %q", closing.AutoRejectedBy, gate.AutoRejectedByCriterion)
	}
	if !strings.Contains(closing.Reason, failing) {
		t.Errorf("the close event's reason does not name the failing criterion %s: %s", failing, closing.Reason)
	}
	if !strings.Contains(closing.Reason, string(criterion.OutcomeFailed)) {
		t.Errorf("the close event's reason does not name the outcome: %s", closing.Reason)
	}
}

// TestAnUnreliableCriterionsFailureDoesNotRejectAtTheMergeRow is the exception
// [criterion.Outcome.Blocks] already carries: while a criterion is unreliable,
// its failure blocks nothing at this row, the same read Merge to master has
// always made of the field — this only adds the call that reaches it.
func TestAnUnreliableCriterionsFailureDoesNotRejectAtTheMergeRow(t *testing.T) {
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
	if len(c.criteria) == 0 {
		t.Fatal("the candidate environment decided no criteria")
	}
	// The same failure as above, but marked unreliable — the state
	// [path.markUnreliable] would have set had this criterion's own outcome
	// history crossed the service's unreliable bound.
	c.criteria[0].Outcome = criterion.OutcomeFailed
	c.criteria[0].Unreliable = true

	if err := path.mergeGate(ctx, c); err != nil {
		t.Fatalf("the Merge to master gate: %v\noutput so far:\n%s", err, out)
	}

	if c.autoRejected {
		t.Fatalf("an unreliable criterion's failure rejected the row: autoRejectedBy %q", c.autoRejectedBy)
	}
	if !c.queued {
		t.Fatal("the candidate did not reach the merge queue, and its only failure is an unreliable criterion")
	}
}

// TestAFailedCriterionAtMergeSendsTheItemBackAndBuildsAgain is the defect a
// live run found and [path.mergeUntilQueued] fixes: a candidate the Merge to
// master row rejects on a failed criterion is not left at Implementation for
// good. It is sent back with an attempt counted there, as it always was, and
// now it is built again against what the row found wrong, deployed onto the
// environment it already has, and the row fires again — reaching the merge
// queue once the rebuild's own criteria pass.
func TestAFailedCriterionAtMergeSendsTheItemBackAndBuildsAgain(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	if _, err := run(ctx, d, of(theStatement)); err != nil {
		t.Fatalf("the first run stopped: %v\noutput so far:\n%s", err, out)
	}

	d.in = strings.NewReader(approvals)
	// The model corrupts only the implementer reply that introduces the second
	// item's own criterion, and only the first time — the shape a real defect
	// the criteria catch takes, and the shape a rebuild against the row's own
	// finding fixes.
	model := &criterionOnceFailingModel{inner: &fakeModel{}, sentence: secondCriterionSentence}
	d.model = model
	path := p(ctx, t, d)
	c := authorOne(t, ctx, path, theSecondStatement, out)
	if !model.corrupted {
		t.Fatal("the fake never corrupted the second item's own criterion; this test proves nothing")
	}

	if err := path.candidateEnvironment(ctx, c); err != nil {
		t.Fatalf("the candidate environment: %v\noutput so far:\n%s", err, out)
	}
	failing := ""
	for _, r := range c.criteria {
		if r.Outcome.Blocks(r.Unreliable) {
			failing = r.CriterionID
		}
	}
	if failing == "" {
		t.Fatalf("the corrupted build did not fail its own criterion on the candidate environment: %v", c.criteria)
	}
	envBefore := c.environmentID
	if envBefore == "" {
		t.Fatal("the candidate has no environment to reuse for the rebuild")
	}

	if err := path.mergeUntilQueued(ctx, c); err != nil {
		t.Fatalf("mergeUntilQueued: %v\noutput so far:\n%s", err, out)
	}

	// The implementer was dispatched a second time, told what the row found
	// wrong.
	if len(model.implementerUsers) != 2 {
		t.Fatalf("the implementer was dispatched %d time(s), want 2 — the failing attempt and the rebuild", len(model.implementerUsers))
	}
	if !strings.Contains(model.implementerUsers[1], "What was found wrong: ") || !strings.Contains(model.implementerUsers[1], failing) {
		t.Errorf("the second attempt was not told what the Merge to master row found wrong (want it to name %s):\n%s",
			failing, model.implementerUsers[1])
	}

	// The item's implementation attempt count rose by one: one entry for the
	// failing attempt authorOne made, one for the rebuild.
	stages, err := item.Stages(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the item's stages: %v", err)
	}
	for _, st := range stages {
		if st.Stage == item.StageImplementation && st.Attempts != 2 {
			t.Errorf("implementation attempts = %d, want 2 — the rejected attempt and the rebuild", st.Attempts)
		}
	}

	// The rebuild ran on the environment the candidate already had, recomposed
	// rather than composed a second time.
	if c.environmentID != envBefore {
		t.Errorf("the rebuild's environment is %s, want the reused %s", c.environmentID, envBefore)
	}

	// The second merge-row close event approves, and the item is in the queue.
	if !c.queued {
		t.Fatalf("the candidate did not reach the merge queue after building again against what was found wrong:\n%s", out)
	}
	closing := closingOf(t, ctx, d, c.mergeGate.closing)
	if closing.Verdict != score.VerdictApproved {
		t.Errorf("the second Merge to master close event is %q, want %q", closing.Verdict, score.VerdictApproved)
	}
	members, err := path.queue.Members(ctx, theServiceRecord(t, ctx, path).ID)
	if err != nil {
		t.Fatalf("reading the queue's members: %v", err)
	}
	found := false
	for _, m := range members {
		if m.ID == c.itemID {
			found = true
		}
	}
	if !found {
		t.Errorf("item %s is not a member of the merge queue after its rebuild was approved", c.itemID)
	}

	if !strings.Contains(out.String(), "goes back to implementation against what the Merge to master row found wrong") {
		t.Errorf("the run does not print the re-entry line:\n%s", out)
	}
}

// closingOf is the closing payload of a close event, found by the id
// [fired.closing] holds.
func closingOf(t *testing.T, ctx context.Context, d deps, closingID string) gate.ClosingPayload {
	t.Helper()
	for _, row := range readLog(t, ctx, d) {
		if row.ID == closingID {
			return closingPayload(t, row)
		}
	}
	t.Fatalf("the log holds no row %s", closingID)
	return gate.ClosingPayload{}
}
