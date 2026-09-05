// [gate.Gate.Fire] over one item's build: what the score and the policy are
// asked, the criteria counted into it, and what the opening payload stores.
package gate_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
)

// TestFireThenApproveIsTwoChainedRows is the demonstration a human at the row
// leaves: the gate fires over a number above the threshold, the owner approves,
// and the log reads back as two chained rows with the chain verifying clean.
func TestFireThenApproveIsTwoChainedRows(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, g := newGate(t, s, p)

	opened, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if !opened.HumanDecides {
		t.Fatalf("a number of 0.6 against a threshold of 0.3 put no human at the row")
	}
	if opened.WhyHuman != gate.WhyOverThreshold {
		t.Errorf("the firing says %q put a human there, want %q", opened.WhyHuman, gate.WhyOverThreshold)
	}
	closing, err := g.Decide(ctx, opened, owner, gate.VerdictApprove, "")
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}

	if err := decisionlog.Verify(ctx, pool); err != nil {
		t.Fatalf("Verify after fire and decide: %v", err)
	}

	rows, err := decisionlog.Read(ctx, pool)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("the log holds %d rows, want 2", len(rows))
	}
	opening := rows[0]
	if opening.ID != opened.Row.ID || rows[1].ID != closing.ID {
		t.Fatalf("the rows read back are not the two appended")
	}
	if opening.Shape != decisionlog.ShapeDecision || opening.Part != decisionlog.PartOpen {
		t.Errorf("the first row is shape %q part %q, want an opening decision row", opening.Shape, opening.Part)
	}
	if rows[1].Closes != opening.ID {
		t.Errorf("the closing closes %q, want the opening %q", rows[1].Closes, opening.ID)
	}
	if rows[1].PrevHash != opening.Hash {
		t.Errorf("the closing names predecessor %q, want the opening's hash %q", rows[1].PrevHash, opening.Hash)
	}
	if opening.Actor.Kind != record.KindComponent || opening.Actor.Name != "gate.merge_to_master" {
		t.Errorf("the opening's actor is %s %q, want component gate.merge_to_master", opening.Actor.Kind, opening.Actor.Name)
	}
	if rows[1].Actor != owner {
		t.Errorf("the closing's actor is %+v, want the deciding human %+v", rows[1].Actor, owner)
	}
	if opening.ScoreVersion != testScoreVersion || opening.PolicyVersion != testPolicyVersion {
		t.Errorf("the opening names score version %q and policy version %q, want %q and %q",
			opening.ScoreVersion, opening.PolicyVersion, testScoreVersion, testPolicyVersion)
	}

	var payload gate.ClosingPayload
	if err := json.Unmarshal([]byte(rows[1].Payload), &payload); err != nil {
		t.Fatalf("unmarshalling the closing payload: %v", err)
	}
	if payload.Verdict != string(gate.VerdictApprove) || payload.Feedback != "" || payload.WhyItAutoPassed != "" {
		t.Errorf("the closing says %+v, want a human's approve with no feedback and nothing auto-passing it", payload)
	}
}

// TestTheOpeningPayloadNamesTheValuesApplied: the open event carries the whole
// vector, the number, the threshold it was compared against, where that
// threshold came from, and what put a human at the row — which is what makes the
// decision readable against the policy it was taken under rather than today's.
func TestTheOpeningPayloadNamesTheValuesApplied(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, g := newGate(t, s, p)

	opened, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}

	rows, err := decisionlog.Read(ctx, pool)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("the log holds %d rows, want 1", len(rows))
	}

	var payload gate.OpeningPayload
	if err := json.Unmarshal([]byte(rows[0].Payload), &payload); err != nil {
		t.Fatalf("unmarshalling the opening payload: %v", err)
	}
	if payload.Gate != string(gate.MergeToMaster) {
		t.Errorf("the payload names gate %q, want %q", payload.Gate, gate.MergeToMaster)
	}
	if payload.ItemID != mergeFiring.ItemID || payload.ArtifactID != mergeFiring.ArtifactID {
		t.Errorf("the payload names item %q artifact %q, want %q %q",
			payload.ItemID, payload.ArtifactID, mergeFiring.ItemID, mergeFiring.ArtifactID)
	}
	if payload.BuildID != mergeFiring.BuildID || payload.ServiceID != mergeFiring.ServiceID ||
		payload.AreaID != mergeFiring.AreaID || payload.EnvironmentID != mergeFiring.EnvironmentID {
		t.Errorf("the payload names build %q service %q area %q environment %q",
			payload.BuildID, payload.ServiceID, payload.AreaID, payload.EnvironmentID)
	}
	if len(payload.Criteria) != len(mergeFiring.Criteria) {
		t.Fatalf("the payload names %d criteria results, want %d", len(payload.Criteria), len(mergeFiring.Criteria))
	}
	if len(payload.Vector) != len(opened.Assessment.Vector) {
		t.Errorf("the payload's vector has %d factors, the assessment's has %d",
			len(payload.Vector), len(opened.Assessment.Vector))
	}
	if payload.Number != 0.6 || payload.Threshold != 0.3 {
		t.Errorf("the payload says number %v against threshold %v, want 0.6 against 0.3", payload.Number, payload.Threshold)
	}
	if payload.ThresholdFrom != string(policy.FromSupplied) {
		t.Errorf("the payload says the threshold was %q, want %q", payload.ThresholdFrom, policy.FromSupplied)
	}
	if !payload.HumanDecides || payload.WhyHuman != gate.WhyOverThreshold {
		t.Errorf("the payload says human_decides %v because %q", payload.HumanDecides, payload.WhyHuman)
	}
	if payload.WaitsOn != gate.WaitsOn(gate.MergeToMaster) {
		t.Errorf("the payload waits on %q, want %q", payload.WaitsOn, gate.WaitsOn(gate.MergeToMaster))
	}
	if payload.FormulaVersion != score.FormulaVersion {
		t.Errorf("the payload names formula %q, want %q", payload.FormulaVersion, score.FormulaVersion)
	}

	// What the gate handed the score and the policy: the records the firing
	// knew, the criteria as two counts, and the measurement it was given.
	if s.asked.ItemID != mergeFiring.ItemID || s.asked.ServiceID != mergeFiring.ServiceID ||
		s.asked.AreaID != mergeFiring.AreaID {
		t.Errorf("the score was asked about %+v", s.asked)
	}
	if s.asked.CriteriaInForce != 2 || s.asked.CriteriaFailed != 0 {
		t.Errorf("the score was told %d criteria in force and %d failed, want 2 and 0",
			s.asked.CriteriaInForce, s.asked.CriteriaFailed)
	}
	if s.asked.Measurement != mergeFiring.Measurement {
		t.Errorf("the score was given measurement %+v, want %+v", s.asked.Measurement, mergeFiring.Measurement)
	}
	if p.asked.GateRow != string(gate.MergeToMaster) || p.asked.EnvironmentID != mergeFiring.EnvironmentID ||
		p.asked.ServiceID != mergeFiring.ServiceID || p.asked.AreaID != mergeFiring.AreaID {
		t.Errorf("the policy was asked about %+v", p.asked)
	}
}

// TestAFailedCriterionReachesTheScoreAsACount: the gate reduces the results to
// how many decided the build and how many failed, which is what the score's
// coverage factor reads.
func TestAFailedCriterionReachesTheScoreAsACount(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, _, g := newGate(t, s, p)

	firing := mergeFiring
	firing.Criteria = []gate.CriterionResult{
		{CriterionID: "cr_a", Outcome: criterion.OutcomePassed},
		{CriterionID: "cr_b", Outcome: criterion.OutcomeFailed},
	}
	if _, err := g.Fire(ctx, firing); err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if s.asked.CriteriaInForce != 2 || s.asked.CriteriaFailed != 1 {
		t.Errorf("the score was told %d in force and %d failed, want 2 and 1",
			s.asked.CriteriaInForce, s.asked.CriteriaFailed)
	}
}

// TestAnUndecidedCriterionReachesTheScoreLikeAFailure: undecided is read at the
// Merge to master gate the way a failure is, which is the whole reason the value exists —
// an encoding that produced a failure and a pass over the same build decided
// nothing.
func TestAnUndecidedCriterionReachesTheScoreLikeAFailure(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, _, g := newGate(t, s, p)

	firing := mergeFiring
	firing.Criteria = []gate.CriterionResult{
		{CriterionID: "cr_a", Outcome: criterion.OutcomePassed},
		{CriterionID: "cr_b", Outcome: criterion.OutcomeUndecided},
	}
	if _, err := g.Fire(ctx, firing); err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if s.asked.CriteriaFailed != 1 {
		t.Errorf("the score was told %d criteria failed, want the undecided one counted as 1", s.asked.CriteriaFailed)
	}
}

// TestTheCandidateDeployRowNamesNoOutcome: at that row the count is known and no
// outcome is, the run that decides them being what the deploy is for — so the
// coverage factor reads the count and the payload carries no result.
func TestTheCandidateDeployRowNamesNoOutcome(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.2)}, &fakePolicy{applied: applied(0.5)}
	ctx, pool, g := newGate(t, s, p)

	firing := mergeFiring
	firing.Row = gate.DeployToCandidateEnvironment
	firing.ArtifactID = ""
	firing.Criteria = nil
	firing.CriteriaInForce = 2
	opened, err := g.Fire(ctx, firing)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if s.asked.CriteriaInForce != 2 || s.asked.CriteriaFailed != 0 {
		t.Errorf("the score was told %d in force and %d failed, want 2 and 0",
			s.asked.CriteriaInForce, s.asked.CriteriaFailed)
	}
	rows, err := decisionlog.Read(ctx, pool)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var payload gate.OpeningPayload
	if err := json.Unmarshal([]byte(rows[len(rows)-1].Payload), &payload); err != nil {
		t.Fatalf("unmarshalling the opening payload: %v", err)
	}
	if len(payload.Criteria) != 0 {
		t.Errorf("the payload names %d criteria results at the candidate deploy row, want none", len(payload.Criteria))
	}
	if opened.HumanDecides {
		t.Error("the candidate deploy row put a human there with the number under the threshold")
	}
}

// TestAnUnavailableFactorGatesTheChange: the score reduces a vector with an
// unavailable factor to the top of the scale, so the number is at or above every
// threshold an owner may author and a human decides. The formula is what carries
// that; this is the gate's half of it.
func TestAnUnavailableFactorGatesTheChange(t *testing.T) {
	assessment := assessed(1)
	assessment.Vector[0].Unavailable = "the diff against master could not be taken"
	assessment.Vector[0].Level = 1
	s, p := &fakeScore{assessment: assessment}, &fakePolicy{applied: applied(1)}
	ctx, pool, g := newGate(t, s, p)

	opened, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if !opened.HumanDecides {
		t.Fatal("a vector with an unavailable factor auto-passed against a threshold of 1")
	}

	rows, err := decisionlog.Read(ctx, pool)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var payload gate.OpeningPayload
	if err := json.Unmarshal([]byte(rows[0].Payload), &payload); err != nil {
		t.Fatalf("unmarshalling the opening payload: %v", err)
	}
	if len(payload.Unavailable) != 1 || payload.Unavailable[0] != "change.size" {
		t.Errorf("the payload names %v as unavailable, want change.size", payload.Unavailable)
	}
	for _, f := range payload.Vector {
		if f.Name == "change.size" && f.Unavailable == "" {
			t.Error("the stored vector does not say why change.size was unavailable")
		}
	}
}

// TestAnIncompleteFiringIsRefused: every row names an item, a build, a service,
// and the environment whose threshold decides it; the merge row also names the
// artifact under decision and the deploy row names none.
func TestAnIncompleteFiringIsRefused(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, g := newGate(t, s, p)

	for _, c := range []struct {
		name   string
		firing func() gate.Firing
	}{
		{"no build", func() gate.Firing { f := mergeFiring; f.BuildID = ""; return f }},
		{"no item", func() gate.Firing { f := mergeFiring; f.ItemID = ""; return f }},
		{"no service", func() gate.Firing { f := mergeFiring; f.ServiceID = ""; return f }},
		{"no environment", func() gate.Firing { f := mergeFiring; f.EnvironmentID = ""; return f }},
		{"no artifact at the merge row", func() gate.Firing { f := mergeFiring; f.ArtifactID = ""; return f }},
		{"an artifact at the deploy row", func() gate.Firing { f := deployFiring(); f.ArtifactID = "art_x"; return f }},
	} {
		if _, err := g.Fire(ctx, c.firing()); !errors.Is(err, gate.ErrFiringIncomplete) {
			t.Errorf("Fire(%s) = %v, want ErrFiringIncomplete", c.name, err)
		}
	}
	if _, err := g.Fire(ctx, gate.Firing{Row: "deploy_to_staging"}); !errors.Is(err, gate.ErrRowUnknown) {
		t.Errorf("Fire of a row this milestone does not build = %v, want ErrRowUnknown", err)
	}

	rows, err := decisionlog.Read(ctx, pool)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("the log holds %d rows after refused firings, want none", len(rows))
	}
}
