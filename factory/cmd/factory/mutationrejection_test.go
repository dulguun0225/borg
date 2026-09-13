package main

import (
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/score"
)

// TestMergeToMasterRejectsCandidateOnAnUncaughtMutant proves the whole path
// from the candidate run to Merge to master: the encodings pass, a changed line
// outside their paths stays uncaught, and the mutation floor rejects before a
// verdict, a queue member, a release, or a master fast-forward exists.
func TestMergeToMasterRejectsCandidateOnAnUncaughtMutant(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	if _, err := run(ctx, d, of(theStatement)); err != nil {
		t.Fatalf("the first run stopped: %v\noutput so far:\n%s", err, out)
	}
	d.model = &fakeModel{failEvery: 1}
	path := p(ctx, t, d)
	svc := theServiceRecord(t, ctx, path)
	if _, err := newFactory(d.pool, d.token).AuthorMutationFloor(ctx,
		owner(t, ctx, d.pool, d.token, d.human), svc.ID, 1); err != nil {
		t.Fatalf("authoring the mutation floor: %v", err)
	}
	if _, err := newFactory(d.pool, d.token).AuthorMutantCap(ctx,
		owner(t, ctx, d.pool, d.token, d.human), svc.ID, 2); err != nil {
		t.Fatalf("authoring the mutation cap: %v", err)
	}

	d.decide = scriptedAtWork(approvals).decide
	c := authorOne(t, ctx, path, theSecondStatement, out)
	if err := path.candidateEnvironment(ctx, c); err != nil {
		t.Fatalf("the candidate environment: %v\noutput so far:\n%s", err, out)
	}
	if len(c.criteria) == 0 {
		t.Fatal("the candidate run decided no criteria")
	}
	for _, result := range c.criteria {
		if result.Outcome.Blocks(result.Unreliable) {
			t.Fatalf("criterion %s did not pass before mutation was read: %s", result.CriterionID, result.Outcome)
		}
	}
	if !c.mutation.Derived() || c.mutation.MutantsTested == 0 || c.mutation.MutantsDetected >= c.mutation.MutantsTested {
		t.Fatalf("candidate mutation = %+v, want a derived reading with an uncaught mutant", c.mutation)
	}

	applicability, reading, err := criterion.LatestMutation(ctx, d.pool, c.buildID, false)
	if err != nil {
		t.Fatalf("reading the candidate mutation: %v", err)
	}
	if applicability != criterion.MutationApplicable || reading.Mutation != c.mutation {
		t.Fatalf("stored mutation = %s %+v, want %s %+v", applicability, reading.Mutation, criterion.MutationApplicable, c.mutation)
	}

	if err := path.mergeGate(ctx, c); err != nil {
		t.Fatalf("the Merge to master gate: %v\noutput so far:\n%s", err, out)
	}
	if !c.autoRejected || c.autoRejectedBy != gate.AutoRejectedByMutationFloor {
		t.Fatalf("the candidate was rejected by %q (auto %v), want %q", c.autoRejectedBy, c.autoRejected, gate.AutoRejectedByMutationFloor)
	}
	if c.queued || c.merged || c.releaseID != "" || c.reverifiedBuildID != "" {
		t.Fatalf("the rejected candidate advanced: queued %v, merged %v, release %q, reverified build %q", c.queued, c.merged, c.releaseID, c.reverifiedBuildID)
	}
	members, err := path.queue.Members(ctx, svc.ID)
	if err != nil {
		t.Fatalf("reading the merge queue: %v", err)
	}
	for _, member := range members {
		if member.ID == c.itemID {
			t.Fatalf("the rejected candidate %s reached the merge queue", c.itemID)
		}
	}
	it, err := item.Get(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}
	if it.Stage != item.StageImplementation {
		t.Errorf("the rejected item is at %s, want implementation", it.Stage)
	}
	if !strings.Contains(c.mergeRejectReason, "below floor") {
		t.Errorf("mutation rejection reason = %q, want the floor", c.mergeRejectReason)
	}
	closing := closingOf(t, ctx, d, c.mergeGate.closing)
	if closing.Verdict != score.VerdictRejected || closing.AutoRejectedBy != gate.AutoRejectedByMutationFloor {
		t.Errorf("close event = %+v, want mutation-floor rejection", closing)
	}
}
