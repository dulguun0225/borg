// [gate.Gate.Fire] over one item's build: what the score and the policy are
// asked, the criteria counted into it, and what the opening payload stores.
package gate_test

import (
	"encoding/json"
	"errors"
	"slices"
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
	ctx, pool, token, g := newGate(t, s, p)

	opened, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if !opened.HumanDecides {
		t.Fatalf("a number of 0.6 against a threshold of 0.3 put no human at the row")
	}
	if !slices.Contains(opened.Marks, gate.MarkTheNumber) {
		t.Errorf("the firing's marks are %v, want them to include the number", opened.Marks)
	}
	closing, err := g.Decide(ctx, opened, gate.Given{Actor: owner, Verdict: gate.VerdictApprove})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}

	if err := decisionlog.NewReader(pool, token).Verify(ctx, ownerReading); err != nil {
		t.Fatalf("Verify after fire and decide: %v", err)
	}

	// Fire's own check that nothing is already pending reads the log first,
	// which appends a read event ahead of the opening; Verify and this Read
	// each append one more, so the log holds five rows and not the two this
	// decision appended.
	rows, err := decisionlog.NewReader(pool, token).Read(ctx, ownerReading)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(rows) != 5 {
		t.Fatalf("the log holds %d rows, want 5", len(rows))
	}
	opening := rowByID(t, rows, opened.Row.ID)
	closingRow := rowByID(t, rows, closing.ID)
	if opening.Shape != decisionlog.ShapeDecision || opening.Part != decisionlog.PartOpen {
		t.Errorf("the opening is shape %q part %q, want an opening decision row", opening.Shape, opening.Part)
	}
	if closingRow.Closes != opening.ID {
		t.Errorf("the closing closes %q, want the opening %q", closingRow.Closes, opening.ID)
	}
	if closingRow.PrevHash != opening.Hash {
		t.Errorf("the closing names predecessor %q, want the opening's hash %q", closingRow.PrevHash, opening.Hash)
	}
	if opening.Actor.Kind != record.KindComponent || opening.Actor.Key != "gate.merge_to_master" {
		t.Errorf("the opening's actor is %s %q, want component gate.merge_to_master", opening.Actor.Kind, opening.Actor.Key)
	}
	if closingRow.Actor != owner {
		t.Errorf("the closing's actor is %+v, want the deciding human %+v", closingRow.Actor, owner)
	}
	if opening.ScoreVersion != testScoreVersion || opening.PolicyVersion != testPolicyVersion {
		t.Errorf("the opening names score version %q and policy version %q, want %q and %q",
			opening.ScoreVersion, opening.PolicyVersion, testScoreVersion, testPolicyVersion)
	}

	var payload gate.ClosingPayload
	if err := json.Unmarshal([]byte(closingRow.Payload), &payload); err != nil {
		t.Fatalf("unmarshalling the closing payload: %v", err)
	}
	if payload.Verdict != string(gate.VerdictApprove) || payload.Reason != "" || payload.WhyItAutoPassed != "" {
		t.Errorf("the closing says %+v, want a human's approve with no reason and nothing auto-passing it", payload)
	}
}

// TestTheOpeningPayloadNamesTheValuesApplied: the open event carries the whole
// vector, the number, the threshold it was compared against, where that
// threshold came from, and what put a human at the row — which is what makes the
// decision readable against the policy it was taken under rather than today's.
func TestTheOpeningPayloadNamesTheValuesApplied(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, token, g := newGate(t, s, p)

	opened, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}

	// Fire's own check that nothing is already pending reads the log first,
	// which appends a read event ahead of the opening; this Read appends one
	// more, so the log holds three rows and not the one this firing appended.
	rows, err := decisionlog.NewReader(pool, token).Read(ctx, ownerReading)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("the log holds %d rows, want 3", len(rows))
	}

	var payload gate.OpeningPayload
	if err := json.Unmarshal([]byte(rowByID(t, rows, opened.Row.ID).Payload), &payload); err != nil {
		t.Fatalf("unmarshalling the opening payload: %v", err)
	}
	if payload.Gate != gate.MergeToMaster.String() {
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
	if !payload.HumanDecides || !slices.Contains(payload.Marks, gate.MarkTheNumber) {
		t.Errorf("the payload says human_decides %v with marks %v, want the number among them", payload.HumanDecides, payload.Marks)
	}
	if payload.WaitsOn.Duty != gate.DutyUAT {
		t.Errorf("the merge row's open event waits on duty %d, want %d (UAT)", payload.WaitsOn.Duty, gate.DutyUAT)
	}
	if payload.FormulaVersion != score.FormulaVersion {
		t.Errorf("the payload names formula %q, want %q", payload.FormulaVersion, score.FormulaVersion)
	}

	// What the gate handed the score and the policy: the records the firing
	// knew and the measurement it was given. Test coverage is deliberately not a
	// factor, so the criteria results reach the payload and not the score.
	if s.asked.ItemID != mergeFiring.ItemID || s.asked.ServiceID != mergeFiring.ServiceID ||
		s.asked.AreaID != mergeFiring.AreaID {
		t.Errorf("the score was asked about %+v", s.asked)
	}
	if s.asked.Measurement != mergeFiring.Measurement {
		t.Errorf("the score was given measurement %+v, want %+v", s.asked.Measurement, mergeFiring.Measurement)
	}
	if p.asked.GateRow != gate.MergeToMaster.String() || p.asked.EnvironmentID != mergeFiring.EnvironmentID ||
		p.asked.ServiceID != mergeFiring.ServiceID || p.asked.AreaID != mergeFiring.AreaID {
		t.Errorf("the policy was asked about %+v", p.asked)
	}
	if payload.CriteriaInForce != 2 || payload.CriteriaFailed != 0 {
		t.Errorf("the payload says %d criteria in force and %d failed, want 2 and 0",
			payload.CriteriaInForce, payload.CriteriaFailed)
	}
}

// TestAFailedCriterionReachesTheOpeningPayloadAsACount: the gate reduces the
// results to how many decided the build and how many failed, which is what a
// human at the merge row reads about the run — nothing in the score reads it,
// test coverage being deliberately not a factor.
func TestAFailedCriterionReachesTheOpeningPayloadAsACount(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, token, g := newGate(t, s, p)

	firing := mergeFiring
	firing.Criteria = []gate.CriterionResult{
		{CriterionID: "cr_a", Outcome: criterion.OutcomePassed},
		{CriterionID: "cr_b", Outcome: criterion.OutcomeFailed},
	}
	if _, err := g.Fire(ctx, firing); err != nil {
		t.Fatalf("Fire: %v", err)
	}
	payload := lastOpeningPayload(t, ctx, pool, token)
	if payload.CriteriaInForce != 2 || payload.CriteriaFailed != 1 {
		t.Errorf("the payload says %d in force and %d failed, want 2 and 1",
			payload.CriteriaInForce, payload.CriteriaFailed)
	}
}

// TestAnUndecidedCriterionReachesTheOpeningPayloadLikeAFailure: undecided is
// read at the Merge to master gate the way a failure is, which is the whole
// reason the value exists — an encoding that produced a failure and a pass over
// the same build decided nothing.
func TestAnUndecidedCriterionReachesTheOpeningPayloadLikeAFailure(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, token, g := newGate(t, s, p)

	firing := mergeFiring
	firing.Criteria = []gate.CriterionResult{
		{CriterionID: "cr_a", Outcome: criterion.OutcomePassed},
		{CriterionID: "cr_b", Outcome: criterion.OutcomeUndecided},
	}
	if _, err := g.Fire(ctx, firing); err != nil {
		t.Fatalf("Fire: %v", err)
	}
	payload := lastOpeningPayload(t, ctx, pool, token)
	if payload.CriteriaFailed != 1 {
		t.Errorf("the payload says %d criteria failed, want the undecided one counted as 1", payload.CriteriaFailed)
	}
}

// TestTheCandidateDeployRowNamesNoOutcome: at that row the count is known and no
// outcome is, the run that decides them being what the deploy is for — so the
// payload carries the count and no result.
func TestTheCandidateDeployRowNamesNoOutcome(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.2)}, &fakePolicy{applied: applied(0.5)}
	ctx, pool, token, g := newGate(t, s, p)

	firing := mergeFiring
	firing.Row = gate.DeployToCandidateEnvironment
	firing.ArtifactID = ""
	firing.Criteria = nil
	firing.CriteriaInForce = 2
	opened, err := g.Fire(ctx, firing)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	// Read appends its own read event after the opening, so the opening is still
	// the first row and not the last.
	payload := lastOpeningPayload(t, ctx, pool, token)
	if payload.CriteriaInForce != 2 || len(payload.Criteria) != 0 {
		t.Errorf("the payload names %d in force and %d results at the candidate deploy row, want the count and no result",
			payload.CriteriaInForce, len(payload.Criteria))
	}
	if opened.HumanDecides {
		t.Error("the candidate deploy row put a human there with the number under the threshold")
	}
}

// TestAResolvedFactorPutsAHumanAtTheRowWhateverTheNumberReads: a factor the
// score could not compute is resolved rather than valued, which is left out of
// the weighted means and puts a human at the row whatever the number reads — a
// low number included, which is what tells the two apart from an unavailable
// factor valued at the top of the scale.
func TestAResolvedFactorPutsAHumanAtTheRowWhateverTheNumberReads(t *testing.T) {
	assessment := assessed(0.1)
	assessment.Resolved = []score.Resolution{
		{Factor: "change.size", Cause: score.CauseUnavailable, Why: "the diff against master could not be taken"},
	}
	assessment.Vector[0].Resolved = string(score.CauseUnavailable)
	s, p := &fakeScore{assessment: assessment}, &fakePolicy{applied: applied(0.9)}
	ctx, pool, token, g := newGate(t, s, p)

	opened, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if !opened.HumanDecides || !slices.Contains(opened.Marks, gate.MarkResolvedFactor) {
		t.Fatalf("a resolved factor did not put a human at the row: human=%v marks=%v", opened.HumanDecides, opened.Marks)
	}

	payload := lastOpeningPayload(t, ctx, pool, token)
	if len(payload.Resolutions) != 1 || payload.Resolutions[0].Factor != "change.size" {
		t.Errorf("the payload names %v as resolved, want change.size", payload.Resolutions)
	}
	for _, f := range payload.Vector {
		if f.Name == "change.size" && f.Resolved == "" {
			t.Error("the stored vector does not say why change.size was resolved")
		}
	}
}

// TestAnIncompleteFiringIsRefused: every row names an item, a build, a service,
// and the environment whose threshold decides it; the implementation row also
// names the artifact under decision, and the merge and deploy rows name none —
// [Row.ArtifactGate] is which.
func TestAnIncompleteFiringIsRefused(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, token, g := newGate(t, s, p)

	for _, c := range []struct {
		name   string
		firing func() gate.Firing
	}{
		{"no build", func() gate.Firing { f := mergeFiring; f.BuildID = ""; return f }},
		{"no item", func() gate.Firing { f := mergeFiring; f.ItemID = ""; return f }},
		{"no service", func() gate.Firing { f := mergeFiring; f.ServiceID = ""; return f }},
		{"no environment", func() gate.Firing { f := mergeFiring; f.EnvironmentID = ""; return f }},
		{"no artifact at the implementation row", func() gate.Firing { f := mergeFiring; f.Row = gate.Implementation; f.ArtifactID = ""; return f }},
		// complete() refuses this before anything is read, so the deploy row's
		// firing is built by hand rather than through deployFiring, which would
		// write a service and an environment this refusal never reaches.
		{"an artifact at the deploy row", func() gate.Firing {
			f := mergeFiring
			f.Row = gate.DeployToProduction
			f.ArtifactID = "art_x"
			return f
		}},
	} {
		if _, err := g.Fire(ctx, c.firing()); !errors.Is(err, gate.ErrFiringIncomplete) {
			t.Errorf("Fire(%s) = %v, want ErrFiringIncomplete", c.name, err)
		}
	}
	if _, err := g.Fire(ctx, gate.Firing{Row: gate.Of(gate.Kind("deploy_to_staging"))}); !errors.Is(err, gate.ErrRowUnknown) {
		t.Errorf("Fire of a row this milestone does not build = %v, want ErrRowUnknown", err)
	}

	rows, err := decisionlog.NewReader(pool, token).Read(ctx, ownerReading)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// Read's own read event is the one row a log with every firing refused holds.
	if len(rows) != 1 {
		t.Fatalf("the log holds %d rows after refused firings, want the one Read appended", len(rows))
	}
}
