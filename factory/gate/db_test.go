// The database tests of this package are in gate_test rather than in gate,
// because they open the pool through package postgres. decisionlog's DDL is
// applied here statement by statement rather than through postgres.Apply,
// which stays the one place the whole schema is composed.
//
// The score and the policy are fakes here, and that is deliberate: what this
// package does with a number and a threshold is what these tests are about, and
// the real score reading the whole graph is package score's own demonstration.
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package gate_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/criterion"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
)

// fakeScore answers with one assessment and records what it was asked, so a test
// can assert that the gate handed the score what the firing knew.
type fakeScore struct {
	assessment score.Assessment
	asked      score.Change
	err        error
	// selection is what the sample answers, and askedHoldOut is what it was asked —
	// the two facts the gate hands it, so a test can assert that the safeguard's
	// answer and the number against the threshold both reached the score.
	selection     score.Selection
	askedHoldOut  [2]bool
	askedItemID   string
	selectionErr  error
	holdOutsAsked int
}

func (f *fakeScore) Assess(_ context.Context, c score.Change) (score.Assessment, error) {
	f.asked = c
	return f.assessment, f.err
}

func (f *fakeScore) HoldOut(_ context.Context, itemID string, wouldGate, bySafeguard bool) (score.Selection, error) {
	f.askedItemID, f.askedHoldOut = itemID, [2]bool{wouldGate, bySafeguard}
	f.holdOutsAsked++
	return f.selection, f.selectionErr
}

// fakePolicy answers with one applied policy and records the subjects it was
// asked about.
type fakePolicy struct {
	applied policy.Applied
	asked   policy.Subjects
}

func (f *fakePolicy) AtGate(_ context.Context, s policy.Subjects) (policy.Applied, error) {
	f.asked = s
	return f.applied, nil
}

const (
	testPolicyVersion = "pv_00000000000000000000000000000001"
	testScoreVersion  = "scv_0000000000000000000000000000001"
)

// assessed is an assessment with a number and nothing unavailable, which is what
// most of these tests vary.
func assessed(number float64) score.Assessment {
	return score.Assessment{
		Version:        testScoreVersion,
		FormulaVersion: score.FormulaVersion,
		Number:         number,
		Likelihood:     0.4,
		Impact:         0.5,
		Exposure:       0.3,
		Vector: []score.Factor{
			{Name: "change.size", Group: score.GroupChange, Half: score.HalfLikelihood, Level: 0.1, Weight: 0.3, Reading: "20 lines changed"},
			{Name: "change.reach", Group: score.GroupChange, Half: score.HalfImpact, Level: 0.6, Weight: 0.5, Reading: "2 of the service's 4 files"},
		},
	}
}

// applied is a policy with one threshold and no safeguard.
func applied(threshold float64) policy.Applied {
	return policy.Applied{
		PolicyVersion: testPolicyVersion,
		Threshold:     threshold,
		ThresholdFrom: policy.FromSupplied,
	}
}

// newGate gives a test a schema of its own, the log's DDL applied inside it, and
// a gate over a writer, a score, and a policy. The schema is dropped when the
// test ends, so a rerun on a database a previous run left dirty starts clean.
func newGate(t *testing.T, s gate.Score, p *fakePolicy) (context.Context, *pgxpool.Pool, *gate.Gate) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m2_gate_" + hex.EncodeToString(suffix[:])

	pool, err := postgres.Open(ctx, inSchema(t, postgres.URL(), schema))
	if err != nil {
		t.Fatalf("the database at %s is not reachable, and these tests do not skip: %v", postgres.URL(), err)
	}
	t.Cleanup(func() {
		// t.Context is already cancelled by the time cleanup runs.
		drop, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := pool.Exec(drop, `drop schema if exists `+pgx.Identifier{schema}.Sanitize()+` cascade`); err != nil {
			t.Errorf("dropping schema %s: %v", schema, err)
		}
		pool.Close()
	})

	if _, err := pool.Exec(ctx, `create schema `+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("creating schema %s: %v", schema, err)
	}
	for n, statement := range decisionlog.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying decisionlog statement %d: %v", n+1, err)
		}
	}
	return ctx, pool, gate.New(decisionlog.NewWriter(pool), s, p, gate.NoChecker{})
}

// inSchema points a connection URL at one schema and nothing else, so every
// unqualified name in the DDL and in the writer's statements resolves there.
func inSchema(t *testing.T, base, schema string) string {
	t.Helper()
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parsing %s: %v", base, err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

var owner = record.Actor{Kind: record.KindHuman, Name: "owner"}

// mergeFiring is one Merge to master firing, complete: the item, the build, the
// artifact version under decision, the records the score and the policy are read
// against, and two criteria results.
var mergeFiring = gate.Firing{
	Row:             gate.MergeToMaster,
	ItemID:          "it_0000000000000000000000000000000a",
	BuildID:         "bl_0000000000000000000000000000000a",
	ArtifactID:      "art_000000000000000000000000000000a",
	ServiceID:       "svc_000000000000000000000000000000a",
	AreaID:          "ar_0000000000000000000000000000000a",
	EnvironmentID:   "env_000000000000000000000000000000a",
	CriteriaInForce: 2,
	Criteria: []gate.CriterionResult{
		{CriterionID: "cr_0000000000000000000000000000000a", Outcome: criterion.OutcomePassed},
		{CriterionID: "cr_0000000000000000000000000000000b", Outcome: criterion.OutcomePassed},
	},
	Measurement: score.Measurement{LinesChanged: 20, FilesChanged: 2, FilesInTree: 4},
}

// deployFiring is the same change at the production deploy row: no artifact, the
// row that offers hold.
func deployFiring() gate.Firing {
	f := mergeFiring
	f.Row = gate.DeployToProduction
	f.ArtifactID = ""
	return f
}

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
	if opening.Shape != decisionlog.ShapeDecision || opening.Part != decisionlog.PartOpening {
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

// TestTheOpeningPayloadNamesTheValuesApplied: the opening row carries the whole
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

// TestAnAutoPassIsClosedByTheGateComponent is the milestone's own demonstration
// at the level of one row: the number is under the threshold, no safeguard adds
// a human, and the factory gives the verdict itself.
func TestAnAutoPassIsClosedByTheGateComponent(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.1)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, g := newGate(t, s, p)

	opened, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if opened.HumanDecides || opened.WhyHuman != "" {
		t.Fatalf("a number of 0.1 against a threshold of 0.3 put a human at the row: %q", opened.WhyHuman)
	}
	closing, err := g.AutoPass(ctx, opened)
	if err != nil {
		t.Fatalf("AutoPass: %v", err)
	}
	if closing.Actor.Kind != record.KindComponent || closing.Actor.Name != "gate.merge_to_master" {
		t.Errorf("the closing's actor is %s %q, want the gate component", closing.Actor.Kind, closing.Actor.Name)
	}

	var payload gate.ClosingPayload
	if err := json.Unmarshal([]byte(closing.Payload), &payload); err != nil {
		t.Fatalf("unmarshalling the closing payload: %v", err)
	}
	if payload.Verdict != string(gate.VerdictApprove) || payload.WhyItAutoPassed != score.AutoPassThreshold {
		t.Errorf("the closing says %+v, want an approve auto-passed by the threshold", payload)
	}

	// The opening row of an auto-pass waits on nobody, which is what tells a
	// reader of the log that nothing was ever pending here.
	rows, err := decisionlog.Read(ctx, pool)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var opening gate.OpeningPayload
	if err := json.Unmarshal([]byte(rows[0].Payload), &opening); err != nil {
		t.Fatalf("unmarshalling the opening payload: %v", err)
	}
	if opening.WaitsOn != "" {
		t.Errorf("an auto-passed firing waits on %q, want nothing", opening.WaitsOn)
	}
	if err := decisionlog.Verify(ctx, pool); err != nil {
		t.Fatalf("Verify after an auto-pass: %v", err)
	}
}

// TestASafeguardAddsAHumanWhateverTheNumberReads: a safeguard can only add. The
// number is well under the threshold and a human decides anyway, the reason
// says so, and the factory may not close the decision itself.
func TestASafeguardAddsAHumanWhateverTheNumberReads(t *testing.T) {
	safeguarded := applied(0.3)
	safeguarded.HumanBySafeguard = true
	safeguarded.Safeguards = []string{"sfg_00000000000000000000000000000001"}
	s, p := &fakeScore{assessment: assessed(0.05)}, &fakePolicy{applied: safeguarded}
	ctx, _, g := newGate(t, s, p)

	opened, err := g.Fire(ctx, deployFiring())
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if !opened.HumanDecides || opened.WhyHuman != gate.WhySafeguard {
		t.Fatalf("the safeguard put no human at the row: human %v because %q", opened.HumanDecides, opened.WhyHuman)
	}
	if _, err := g.AutoPass(ctx, opened); !errors.Is(err, gate.ErrHumanDecides) {
		t.Fatalf("AutoPass over a firing a safeguard reached = %v, want ErrHumanDecides", err)
	}
	if _, err := g.Decide(ctx, opened, owner, gate.VerdictApprove, ""); err != nil {
		t.Fatalf("Decide: %v", err)
	}
}

// TestBothReasonsAreToldApart: a number over the threshold and a safeguard at
// once says so, because withdrawing the safeguard would not remove the human.
func TestBothReasonsAreToldApart(t *testing.T) {
	safeguarded := applied(0.3)
	safeguarded.HumanBySafeguard = true
	s, p := &fakeScore{assessment: assessed(0.9)}, &fakePolicy{applied: safeguarded}
	ctx, _, g := newGate(t, s, p)

	opened, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if opened.WhyHuman != gate.WhyBoth {
		t.Errorf("the firing says %q, want both reasons", opened.WhyHuman)
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

// TestEachRowOffersItsOwnActions: reject is available up to the merge to master
// and nowhere after it, and hold is offered by the deploy row alone.
func TestEachRowOffersItsOwnActions(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, _, g := newGate(t, s, p)

	merged, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire at the merge row: %v", err)
	}
	if _, err := g.Decide(ctx, merged, owner, gate.VerdictHold, ""); !errors.Is(err, gate.ErrVerdictUnknown) {
		t.Errorf("holding at the merge row = %v, want ErrVerdictUnknown", err)
	}

	deployed, err := g.Fire(ctx, deployFiring())
	if err != nil {
		t.Fatalf("Fire at the deploy row: %v", err)
	}
	if _, err := g.Decide(ctx, deployed, owner, gate.VerdictReject, "no"); !errors.Is(err, gate.ErrVerdictUnknown) {
		t.Errorf("rejecting at the deploy row = %v, want ErrVerdictUnknown", err)
	}
	if _, err := g.Decide(ctx, deployed, owner, gate.Verdict("edit"), ""); !errors.Is(err, gate.ErrVerdictUnknown) {
		t.Errorf("an action neither row has = %v, want ErrVerdictUnknown", err)
	}
}

// TestAHoldCloses: a hold is the verdict of that firing's decision, with the
// human as the actor, and it says nothing about a stage to return to — the
// change is still good and the event is queued.
func TestAHoldCloses(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, g := newGate(t, s, p)

	opened, err := g.Fire(ctx, deployFiring())
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	closing, err := g.Decide(ctx, opened, owner, gate.VerdictHold, "the dependency is not live")
	if err != nil {
		t.Fatalf("Decide(hold): %v", err)
	}
	if closing.Actor != owner {
		t.Errorf("the hold's actor is %+v, want the human who set it", closing.Actor)
	}

	var payload gate.ClosingPayload
	if err := json.Unmarshal([]byte(closing.Payload), &payload); err != nil {
		t.Fatalf("unmarshalling the closing payload: %v", err)
	}
	if payload.Verdict != string(gate.VerdictHold) {
		t.Errorf("the closing says verdict %q, want a hold", payload.Verdict)
	}
	if payload.ReturnsTo != "" {
		t.Errorf("the hold says the item returns to %q, and a hold sends nothing back", payload.ReturnsTo)
	}
	if payload.WhyItAutoPassed != "" {
		t.Errorf("the hold says it was auto-passed by %q", payload.WhyItAutoPassed)
	}
	if err := decisionlog.Verify(ctx, pool); err != nil {
		t.Fatalf("Verify after a hold: %v", err)
	}
}

// TestARejectNamesTheStageItReturnsTo: the merge row's reject sends the item to
// the nearest authoring stage above it, there being no stage of its own and none
// between.
func TestARejectNamesTheStageItReturnsTo(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, _, g := newGate(t, s, p)

	opened, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	feedback := "the encoding of cr_0000000000000000000000000000000b asserts the code, not the criterion"
	closing, err := g.Decide(ctx, opened, owner, gate.VerdictReject, feedback)
	if err != nil {
		t.Fatalf("Decide(reject, feedback): %v", err)
	}

	var payload gate.ClosingPayload
	if err := json.Unmarshal([]byte(closing.Payload), &payload); err != nil {
		t.Fatalf("unmarshalling the closing payload: %v", err)
	}
	if payload.Verdict != string(gate.VerdictReject) || payload.Feedback != feedback {
		t.Errorf("the closing says %+v", payload)
	}
	if payload.ReturnsTo != gate.ReturnsTo {
		t.Errorf("the reject returns the item to %q, want %q", payload.ReturnsTo, gate.ReturnsTo)
	}
}

// TestARejectWithoutFeedbackIsRefused: the action is "Reject with feedback", so
// a reject carrying none is refused and no closing row is appended.
func TestARejectWithoutFeedbackIsRefused(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, g := newGate(t, s, p)

	opened, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if _, err := g.Decide(ctx, opened, owner, gate.VerdictReject, ""); !errors.Is(err, gate.ErrFeedbackMissing) {
		t.Fatalf("Decide(reject, no feedback) = %v, want ErrFeedbackMissing", err)
	}

	rows, err := decisionlog.Read(ctx, pool)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("the log holds %d rows after the refused reject, want the opening alone", len(rows))
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

// TestEveryRowHasActionsAndAWait: nothing may be fired that has no actions, and
// a row that waits on nobody would leave a pending decision no reader can chase.
func TestEveryRowHasActionsAndAWait(t *testing.T) {
	for _, row := range gate.Rows {
		actions, err := gate.Actions(row)
		if err != nil {
			t.Errorf("Actions(%s) = %v", row, err)
			continue
		}
		if len(actions) < 2 {
			t.Errorf("%s offers %v, and every row approves and does one other thing", row, actions)
		}
		if actions[0] != gate.VerdictApprove {
			t.Errorf("%s does not offer approve first: %v", row, actions)
		}
		if gate.WaitsOn(row) == "" {
			t.Errorf("%s names nothing it waits on", row)
		}
	}
}

// TestApprovalTimesIsWhatOrdersTheMergeQueue: the queue's order is the item's
// priority and then the time of the merge approval in the log, and that time is a
// fact of no record — so this package, which owns the shape of both payloads,
// answers it. A rejected item has no approval, and an item approved twice keeps the
// latest.
func TestApprovalTimesIsWhatOrdersTheMergeQueue(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, g := newGate(t, s, p)
	human := record.Actor{Kind: record.KindHuman, Name: "owner"}

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
		closing, err := g.Decide(ctx, opened, human, verdict, feedback)
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
	if _, err := decisionlog.NewWriter(pool).AppendDecisionOpening(ctx, decisionlog.Entry{
		Actor:         record.Actor{Kind: record.KindComponent, Name: "gate.some_other_gate"},
		Payload:       "a payload this package has no shape for",
		PolicyVersion: testPolicyVersion,
		ScoreVersion:  testScoreVersion,
	}); err != nil {
		t.Fatalf("appending the unreadable opening row: %v", err)
	}

	times, err := gate.ApprovalTimes(ctx, pool, gate.MergeToMaster)
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

	if _, err := gate.ApprovalTimes(ctx, pool, gate.Row("some_other_row")); !errors.Is(err, gate.ErrRowUnknown) {
		t.Errorf("ApprovalTimes at a row this package does not fire = %v, want ErrRowUnknown", err)
	}
}

// TestTheDecompositionRowDecidesOverASetAndAppliesItsRiskiestMember: the one row
// where approving admits several timelines at once, fired over the items decomposition wrote.
func TestTheDecompositionRowDecidesOverASetAndAppliesItsRiskiestMember(t *testing.T) {
	// Two members and two answers: the score is asked per member and the row
	// applies the higher of the numbers, because approving the set approves every
	// item in it.
	s, p := &varyingScore{by: map[string]float64{"it_a": 0.2, "it_b": 0.7}}, &fakePolicy{applied: applied(0.5)}
	ctx, pool, g := newGate(t, s, p)

	opened, err := g.FireSet(ctx, gate.SetFiring{
		IntentID:      "in_0000000000000000000000000000000a",
		EnvironmentID: "env_000000000000000000000000000000a",
		Members: []gate.SetMember{
			{ItemID: "it_a", ServiceID: "svc_a", AreaID: "ar_a"},
			{ItemID: "it_b", ServiceID: "svc_b", AreaID: "ar_a", WaitsOn: []string{"it_a"}},
		},
	})
	if err != nil {
		t.Fatalf("FireSet: %v", err)
	}
	if opened.Gate != gate.Decomposition {
		t.Fatalf("the row is %s", opened.Gate)
	}
	if opened.Assessment.Number != 0.7 || !opened.HumanDecides {
		t.Fatalf("the row applied %v and human=%v, want the riskiest member's number over the threshold",
			opened.Assessment.Number, opened.HumanDecides)
	}

	var payload gate.SetOpeningPayload
	if err := json.Unmarshal([]byte(opened.Row.Payload), &payload); err != nil {
		t.Fatalf("reading the opening payload: %v", err)
	}
	if payload.IntentID != "in_0000000000000000000000000000000a" {
		t.Errorf("the opening row names intent %q", payload.IntentID)
	}
	if len(payload.Set) != 2 {
		t.Fatalf("the opening row carries %d members, want the whole set whichever one drove the number", len(payload.Set))
	}
	if payload.NumberFrom != "it_b" {
		t.Errorf("the number came from %q, want the riskier member", payload.NumberFrom)
	}
	if len(payload.Set[1].WaitsOn) != 1 {
		t.Errorf("the row does not say what waits on what: %+v", payload.Set)
	}
	// The subject a decision names is what the score reads back when it counts
	// outcomes, and this row names none: decomposition proposes a set rather than an
	// artifact, so a verdict here is an outcome on no author's work.
	var opening score.Opening
	if err := json.Unmarshal([]byte(opened.Row.Payload), &opening); err != nil {
		t.Fatalf("reading the payload as an opening: %v", err)
	}
	if opening.ItemID != "" || opening.ArtifactID != "" {
		t.Errorf("the Decomposition row names an item %+v, and decomposition is not an artifact", opening)
	}
	if opening.HeldOut {
		t.Error("the Decomposition row says the score held something out, and the sample does not reach a set")
	}
	// The diff factors are unavailable at decomposition, which the vector says rather
	// than leaving a gap a reader has to interpret.
	if s.asked.Measurement.Unavailable != gate.NoBuildAtDecomposition {
		t.Errorf("the score was asked with measurement %+v", s.asked.Measurement)
	}

	closing, err := g.Decide(ctx, opened, owner, gate.VerdictApprove, "")
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if err := decisionlog.Verify(ctx, pool); err != nil {
		t.Fatalf("the chain does not verify after a set decision: %v", err)
	}
	if closing.Closes != opened.Row.ID {
		t.Errorf("the closing row closes %q", closing.Closes)
	}
}

// TestARejectAtDecompositionNamesNoStage: its reject re-decomposes the set rather than
// sending an item anywhere, so the field its closing row would carry stays unwritten.
func TestARejectAtDecompositionNamesNoStage(t *testing.T) {
	s, p := &varyingScore{by: map[string]float64{"it_a": 0.7, "it_b": 0.7}}, &fakePolicy{applied: applied(0.5)}
	ctx, _, g := newGate(t, s, p)

	opened, err := g.FireSet(ctx, gate.SetFiring{
		IntentID:      "in_0000000000000000000000000000000a",
		EnvironmentID: "env_000000000000000000000000000000a",
		Members: []gate.SetMember{
			{ItemID: "it_a", ServiceID: "svc_a"}, {ItemID: "it_b", ServiceID: "svc_b"},
		},
	})
	if err != nil {
		t.Fatalf("FireSet: %v", err)
	}
	closing, err := g.Decide(ctx, opened, owner, gate.VerdictReject, "this should have been three items")
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	var payload gate.ClosingPayload
	if err := json.Unmarshal([]byte(closing.Payload), &payload); err != nil {
		t.Fatalf("reading the closing payload: %v", err)
	}
	if payload.ReturnsTo != "" {
		t.Errorf("the reject returns the item to %q, and Decomposition names nothing at all", payload.ReturnsTo)
	}
}

// TestASetFiringMissingSomethingIsRefused: the row fires where decomposition yielded more
// than one item, and a firing of one is not an error of shape but of occasion.
func TestASetFiringMissingSomethingIsRefused(t *testing.T) {
	s, p := &varyingScore{by: map[string]float64{}}, &fakePolicy{applied: applied(0.5)}
	ctx, _, g := newGate(t, s, p)

	two := []gate.SetMember{{ItemID: "it_a", ServiceID: "svc_a"}, {ItemID: "it_b", ServiceID: "svc_b"}}
	for name, firing := range map[string]gate.SetFiring{
		"no intent":      {EnvironmentID: "env_a", Members: two},
		"no environment": {IntentID: "in_a", Members: two},
		"one member":     {IntentID: "in_a", EnvironmentID: "env_a", Members: two[:1]},
		"a member with no service": {IntentID: "in_a", EnvironmentID: "env_a",
			Members: []gate.SetMember{{ItemID: "it_a"}, {ItemID: "it_b", ServiceID: "svc_b"}}},
	} {
		if _, err := g.FireSet(ctx, firing); !errors.Is(err, gate.ErrSetIncomplete) {
			t.Errorf("a set firing with %s = %v, want ErrSetIncomplete", name, err)
		}
	}
	// And a Decomposition firing given as an ordinary one is refused: that row
	// decides over a set and not over one item's build.
	f := mergeFiring
	f.Row = gate.Decomposition
	if _, err := g.Fire(ctx, f); !errors.Is(err, gate.ErrFiringIncomplete) {
		t.Errorf("firing Decomposition through Fire = %v, want ErrFiringIncomplete", err)
	}
}

// TestAutoRejectIsTheFactorysOwnAndIsAllowedOverAHuman: the factory may not approve
// over a human and it rejects before one is asked, which is the one asymmetry between
// the two calls.
func TestAutoRejectIsTheFactorysOwnAndIsAllowedOverAHuman(t *testing.T) {
	// A number over the threshold, so the firing puts a human at the row.
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, g := newGate(t, s, p)

	opened, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if !opened.HumanDecides {
		t.Fatal("the firing put no human at the row, and this test is about rejecting over one")
	}
	if _, err := g.AutoPass(ctx, opened); !errors.Is(err, gate.ErrHumanDecides) {
		t.Fatalf("AutoPass over a human = %v, want ErrHumanDecides", err)
	}

	closing, err := g.AutoReject(ctx, opened, gate.AutoRejectedByContractDiff,
		"health.Detail is removed and the reader still declares it")
	if err != nil {
		t.Fatalf("AutoReject: %v", err)
	}
	if closing.Actor.Kind != record.KindComponent || closing.Actor.Name != "gate.merge_to_master" {
		t.Errorf("the closing row was written as %s %s, want the gate component", closing.Actor.Kind, closing.Actor.Name)
	}
	var payload gate.ClosingPayload
	if err := json.Unmarshal([]byte(closing.Payload), &payload); err != nil {
		t.Fatalf("reading the closing payload: %v", err)
	}
	if payload.Verdict != string(gate.VerdictReject) || payload.AutoRejectedBy != gate.AutoRejectedByContractDiff {
		t.Fatalf("the closing row reads %+v", payload)
	}
	if payload.Feedback == "" || payload.ReturnsTo != gate.ReturnsTo {
		t.Errorf("a mechanical reject carries feedback %q and returns to %q", payload.Feedback, payload.ReturnsTo)
	}
	if err := decisionlog.Verify(ctx, pool); err != nil {
		t.Fatalf("the chain does not verify after a mechanical reject: %v", err)
	}

	// A rejection that names no check is refused: it is only readable against the
	// check it came from.
	second, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("the second Fire: %v", err)
	}
	if _, err := g.AutoReject(ctx, second, "", "something"); !errors.Is(err, gate.ErrCheckMissing) {
		t.Errorf("a mechanical reject naming no check = %v, want ErrCheckMissing", err)
	}
	if _, err := g.AutoReject(ctx, second, gate.AutoRejectedByConsumerContract, ""); !errors.Is(err, gate.ErrCheckMissing) {
		t.Errorf("a mechanical reject saying nothing = %v, want ErrCheckMissing", err)
	}
	// And the production deploy row does not reject at all: by then the merge has
	// happened and the number is assigned.
	deploy, err := g.Fire(ctx, deployFiring())
	if err != nil {
		t.Fatalf("firing the deploy row: %v", err)
	}
	if _, err := g.AutoReject(ctx, deploy, gate.AutoRejectedByConsumerContract, "anything"); !errors.Is(err, gate.ErrVerdictUnknown) {
		t.Errorf("a mechanical reject at the production deploy row = %v, want ErrVerdictUnknown", err)
	}
}

// TestEditInPlaceAtDecompositionIsRefusedWithItsReason: re-decomposing is not built, so a
// bad decomposition is rejected rather than repaired, and the vocabulary says so.
func TestEditInPlaceAtDecompositionIsRefusedWithItsReason(t *testing.T) {
	if gate.ErrEditInPlaceRefused == nil {
		t.Fatal("the refusal has no reason to carry")
	}
	actions, err := gate.Actions(gate.Decomposition)
	if err != nil {
		t.Fatalf("Actions: %v", err)
	}
	if len(actions) != 2 {
		t.Fatalf("Decomposition offers %v, want approve and reject — the third action is refused", actions)
	}
}

// varyingScore answers a different number per item, which is what a set firing needs:
// the row applies the riskiest member's.
type varyingScore struct {
	by           map[string]float64
	asked        score.Change
	heldOutAsked []string
}

func (v *varyingScore) Assess(_ context.Context, c score.Change) (score.Assessment, error) {
	v.asked = c
	return assessed(v.by[c.ItemID]), nil
}

// HoldOut selects nothing. The Decomposition row does not ask, and a test that
// drives one asserts that: the sample selects an item and one draw over a set
// would select several on a number that is none of theirs.
func (v *varyingScore) HoldOut(_ context.Context, itemID string, _, _ bool) (score.Selection, error) {
	v.heldOutAsked = append(v.heldOutAsked, itemID)
	return score.Selection{}, nil
}

// TestTheVerdictsGateWritesAreTheOnesTheScoreReads holds two spellings together.
// The score reads a closing row's verdict when it counts outcomes and cannot import
// this package, importing it the other way, so it declares the two words itself —
// and two packages naming one word are two able to disagree.
func TestTheVerdictsGateWritesAreTheOnesTheScoreReads(t *testing.T) {
	if string(gate.VerdictApprove) != score.VerdictApproved {
		t.Errorf("the gate writes %q and the score reads %q", gate.VerdictApprove, score.VerdictApproved)
	}
	if string(gate.VerdictReject) != score.VerdictRejected {
		t.Errorf("the gate writes %q and the score reads %q", gate.VerdictReject, score.VerdictRejected)
	}
}

// TestTheSampleRemovesTheNumbersHumanAndNoOtherIsTheOneAsymmetryHere: the gate
// asks the score's sample with the safeguard's answer and after the independent
// checker's, so a held-out item passes the gate the number would have gated and
// neither of the other two.
func TestTheSampleRemovesTheNumbersHumanAndNoOtherIsTheOneAsymmetryHere(t *testing.T) {
	// Over the threshold and held out: no human, and the closing row says the
	// sample passed it.
	s := &fakeScore{assessment: assessed(0.6), selection: score.Selection{HeldOut: true, Why: score.SelectedHere}}
	ctx, _, g := newGate(t, s, &fakePolicy{applied: applied(0.3)})
	opened, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if opened.HumanDecides {
		t.Errorf("a held-out firing put a human at the row: %s", opened.WhyHuman)
	}
	if !opened.HeldOut || opened.WhyHeldOut != score.SelectedHere {
		t.Errorf("the firing reads held out %v because %q", opened.HeldOut, opened.WhyHeldOut)
	}
	if s.askedHoldOut != [2]bool{true, false} || s.askedItemID != mergeFiring.ItemID {
		t.Errorf("the gate asked the sample about %q with %v, want the item with the number over the threshold and no safeguard",
			s.askedItemID, s.askedHoldOut)
	}
	closing, err := g.AutoPass(ctx, opened)
	if err != nil {
		t.Fatalf("AutoPass: %v", err)
	}
	var payload gate.ClosingPayload
	if err := json.Unmarshal([]byte(closing.Payload), &payload); err != nil {
		t.Fatalf("reading the closing payload: %v", err)
	}
	if payload.WhyItAutoPassed != score.AutoPassSample {
		t.Errorf("the closing row says %q, want the sample", payload.WhyItAutoPassed)
	}

	// A safeguard adds a human whatever the sample answers, and the gate hands the
	// sample the safeguard's answer so that it cannot select at all.
	safeguarded := &fakeScore{assessment: assessed(0.1), selection: score.Selection{HeldOut: true}}
	safeguardedApplied := applied(0.3)
	safeguardedApplied.HumanBySafeguard = true
	ctx, _, g = newGate(t, safeguarded, &fakePolicy{applied: safeguardedApplied})
	opened, err = g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire over a row a safeguard reached: %v", err)
	}
	if !opened.HumanDecides {
		t.Error("a row a safeguard reached auto-passed")
	}
	if safeguarded.askedHoldOut != [2]bool{false, true} {
		t.Errorf("the gate asked the sample with %v, want the safeguard's answer", safeguarded.askedHoldOut)
	}

	// Under the threshold and held out: still held out — the selection is the
	// item's — and the closing row says the threshold, because the score would have
	// passed this one anyway and it is evidence about no gate.
	under := &fakeScore{assessment: assessed(0.1), selection: score.Selection{HeldOut: true, Why: score.SelectedEarlier}}
	ctx, _, g = newGate(t, under, &fakePolicy{applied: applied(0.3)})
	opened, err = g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire under the threshold: %v", err)
	}
	if !opened.HeldOut {
		t.Error("an item selected earlier is not held out here")
	}
	closing, err = g.AutoPass(ctx, opened)
	if err != nil {
		t.Fatalf("AutoPass: %v", err)
	}
	if err := json.Unmarshal([]byte(closing.Payload), &payload); err != nil {
		t.Fatalf("reading the closing payload: %v", err)
	}
	if payload.WhyItAutoPassed != score.AutoPassThreshold {
		t.Errorf("the closing row says %q, want the threshold", payload.WhyItAutoPassed)
	}
}
