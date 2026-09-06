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
