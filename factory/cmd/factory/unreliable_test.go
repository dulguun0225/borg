// Tests of a criterion's unreliable bound read at the gate: crossing it marks
// the criterion, its failure reads as absent the way [criterion.Outcome.Blocks]
// already decides — which is the exact read Merge to master makes — and
// becoming unreliable raises one intent keyed by the criterion.
package main

import (
	"testing"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/intent"
)

// TestAFlappingCriterionIsMarkedUnreliableAndRaisesOneIntent drives a criterion
// across the shipped bound the way test practice calls flaky: two builds that
// disagree out of two decided is a rate the shipped bound of 0.2 cannot clear.
// The fake model's own encodings are deterministic — two candidates of one
// service write identical bytes, per its own doc — so what makes this build's
// history disagree is written directly into the outcome table the way the
// deployer's own run would, and never a fabricated field.
func TestAFlappingCriterionIsMarkedUnreliableAndRaisesOneIntent(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	res, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	shipped := only(t, res)
	if len(shipped.criteria) != 1 {
		t.Fatalf("the run decided %d criteria, want the one statement introduced", len(shipped.criteria))
	}
	criterionID := shipped.criteria[0].CriterionID
	if shipped.criteria[0].Unreliable {
		t.Fatalf("a criterion decided over its only build reads as unreliable, and there is nothing yet to disagree with")
	}

	path := p(ctx, t, d)

	// A second build's own run, disagreeing with the first: the deployer's own
	// writer, over a build this test names rather than one it built, which is
	// exactly the shape the store's own id fields take — checked for being
	// present and never for pointing at anything.
	const secondBuildID = "bl_flapping_second"
	if err := criterion.RecordResults(ctx, d.pool, d.token, deployActor,
		criterion.Run{BuildID: secondBuildID, Number: 1, Place: criterion.PlaceCandidateEnvironment, Composition: "synthetic"},
		map[string]criterion.Outcome{criterionID: criterion.OutcomeFailed},
	); err != nil {
		t.Fatalf("recording the second build's disagreeing result: %v", err)
	}

	results := []gate.CriterionResult{{CriterionID: criterionID, Outcome: criterion.OutcomeFailed}}
	if err := path.markUnreliable(ctx, shipped, secondBuildID, results); err != nil {
		t.Fatalf("markUnreliable: %v", err)
	}
	if !results[0].Unreliable {
		t.Fatal("a criterion disagreeing on one build out of two, a rate of 0.5, does not cross the shipped bound of 0.2")
	}
	// This is the exact read Merge to master makes over every criterion result
	// it holds — [gate.CriterionResult.Outcome.Blocks] — so a failure that does
	// not block here is a failure Merge to master reads as absent.
	if results[0].Outcome.Blocks(results[0].Unreliable) {
		t.Error("an unreliable criterion's failure blocks Merge to master, and while unreliable it rejects nothing")
	}

	waiting, found, err := intent.OnEvidence(ctx, d.pool, intent.Evidence{CriterionID: criterionID})
	if err != nil {
		t.Fatalf("OnEvidence over the criterion: %v", err)
	}
	if !found {
		t.Fatal("becoming unreliable raised no intent keyed by the criterion")
	}
	if waiting.Source != intent.SourceDetector {
		t.Errorf("the intent's source is %s, want detector — becoming unreliable is raised the way a detector raises one", waiting.Source)
	}

	// A second crossing while that intent is open joins it rather than raising
	// a second one.
	const thirdBuildID = "bl_flapping_third"
	if err := criterion.RecordResults(ctx, d.pool, d.token, deployActor,
		criterion.Run{BuildID: thirdBuildID, Number: 1, Place: criterion.PlaceCandidateEnvironment, Composition: "synthetic"},
		map[string]criterion.Outcome{criterionID: criterion.OutcomeFailed},
	); err != nil {
		t.Fatalf("recording the third build's disagreeing result: %v", err)
	}
	moreResults := []gate.CriterionResult{{CriterionID: criterionID, Outcome: criterion.OutcomeFailed}}
	if err := path.markUnreliable(ctx, shipped, thirdBuildID, moreResults); err != nil {
		t.Fatalf("markUnreliable, the second crossing: %v", err)
	}
	if !moreResults[0].Unreliable {
		t.Fatal("the criterion is still unreliable after a second crossing")
	}
	again, found, err := intent.OnEvidence(ctx, d.pool, intent.Evidence{CriterionID: criterionID})
	if err != nil {
		t.Fatalf("OnEvidence over the criterion, the second time: %v", err)
	}
	if !found {
		t.Fatal("the intent the first crossing raised is gone")
	}
	if again.ID != waiting.ID {
		t.Errorf("a second crossing while the intent is open raised a second one, %s, want it to join %s", again.ID, waiting.ID)
	}
}
