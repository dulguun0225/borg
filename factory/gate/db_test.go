// The database tests of this package are in gate_test rather than in gate,
// because they open the pool through package postgres. decisionlog's DDL is
// applied here statement by statement rather than through postgres.Apply,
// which stays the one place the whole schema is composed.
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
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

// newGate gives a test a schema of its own, the log's DDL applied inside it,
// and the gate over a writer and the stub. The schema is dropped when the
// test ends, so a rerun on a database a previous run left dirty starts clean.
func newGate(t *testing.T) (context.Context, *pgxpool.Pool, *gate.Gate) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m1_gate_" + hex.EncodeToString(suffix[:])

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
	return ctx, pool, gate.New(decisionlog.NewWriter(pool), gate.Stub{})
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

// firing is one Merge to master firing, complete: the item, the build, the
// artifact version under decision, and two criteria results.
var firing = gate.Firing{
	ItemID:     "it_0000000000000000000000000000000a",
	BuildID:    "bl_0000000000000000000000000000000a",
	ArtifactID: "art_0000000000000000000000000000000a",
	Criteria: []gate.CriterionResult{
		{CriterionID: "cr_0000000000000000000000000000000a", Passed: true},
		{CriterionID: "cr_0000000000000000000000000000000b", Passed: true},
	},
}

// TestFireThenApproveIsTwoChainedRows is the demonstration: the gate fires,
// the owner approves, and the log reads back as two chained rows — the
// closing naming the opening — with the chain verifying clean.
func TestFireThenApproveIsTwoChainedRows(t *testing.T) {
	ctx, pool, g := newGate(t)

	opened, err := g.Fire(ctx, firing)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if !opened.Assessment.HumanDecides {
		t.Fatalf("the stub's assessment does not put a human at the gate: %+v", opened.Assessment)
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
	if rows[1].Shape != decisionlog.ShapeDecision || rows[1].Part != decisionlog.PartClosing {
		t.Errorf("the second row is shape %q part %q, want a closing decision row", rows[1].Shape, rows[1].Part)
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
	if opening.ScoreVersion != gate.StubVersion {
		t.Errorf("the opening names score version %q, want %q", opening.ScoreVersion, gate.StubVersion)
	}
	if opening.PolicyVersion != gate.PolicyVersion {
		t.Errorf("the opening names policy version %q, want %q", opening.PolicyVersion, gate.PolicyVersion)
	}

	var payload gate.ClosingPayload
	if err := json.Unmarshal([]byte(rows[1].Payload), &payload); err != nil {
		t.Fatalf("unmarshalling the closing payload: %v", err)
	}
	if payload.Verdict != string(gate.VerdictApprove) || payload.Feedback != "" {
		t.Errorf("the closing says %+v, want an approve with no feedback", payload)
	}
}

// TestTheOpeningPayloadNamesWhatWasDecidedOver unmarshals the opening row's
// payload and asserts it names the gate, the artifact version under decision,
// the criteria results, the vector, the number, and what the row waits on.
func TestTheOpeningPayloadNamesWhatWasDecidedOver(t *testing.T) {
	ctx, pool, g := newGate(t)

	opened, err := g.Fire(ctx, firing)
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
	if payload.Gate != gate.MergeToMaster {
		t.Errorf("the payload names gate %q, want %q", payload.Gate, gate.MergeToMaster)
	}
	if payload.ItemID != firing.ItemID || payload.BuildID != firing.BuildID {
		t.Errorf("the payload names item %q build %q, want %q %q",
			payload.ItemID, payload.BuildID, firing.ItemID, firing.BuildID)
	}
	if payload.ArtifactID != firing.ArtifactID {
		t.Errorf("the payload names artifact %q, want the version under decision %q",
			payload.ArtifactID, firing.ArtifactID)
	}
	if len(payload.Criteria) != len(firing.Criteria) {
		t.Fatalf("the payload names %d criteria results, want %d", len(payload.Criteria), len(firing.Criteria))
	}
	for n, result := range payload.Criteria {
		if result != firing.Criteria[n] {
			t.Errorf("criteria result %d is %+v, want %+v", n+1, result, firing.Criteria[n])
		}
	}
	if len(payload.Vector) != len(opened.Assessment.Vector) {
		t.Errorf("the payload's vector has %d factors, the assessment's has %d",
			len(payload.Vector), len(opened.Assessment.Vector))
	}
	if payload.Number != opened.Assessment.Number {
		t.Errorf("the payload's number is %q, the assessment's is %q", payload.Number, opened.Assessment.Number)
	}
	if payload.WaitsOn != gate.WaitsOn {
		t.Errorf("the payload waits on %q, want %q", payload.WaitsOn, gate.WaitsOn)
	}
}

// TestARejectWithoutFeedbackIsRefused: the action is "Reject with feedback",
// so a reject carrying none is refused and no closing row is appended.
func TestARejectWithoutFeedbackIsRefused(t *testing.T) {
	ctx, pool, g := newGate(t)

	opened, err := g.Fire(ctx, firing)
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

// TestARejectWithFeedbackCloses: a reject carrying feedback closes the
// decision, and the closing payload says the verdict and the feedback.
func TestARejectWithFeedbackCloses(t *testing.T) {
	ctx, pool, g := newGate(t)

	opened, err := g.Fire(ctx, firing)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	feedback := "the encoding of cr_0000000000000000000000000000000b asserts the code, not the criterion"
	closing, err := g.Decide(ctx, opened, owner, gate.VerdictReject, feedback)
	if err != nil {
		t.Fatalf("Decide(reject, feedback): %v", err)
	}
	if closing.Closes != opened.Row.ID {
		t.Errorf("the closing closes %q, want the opening %q", closing.Closes, opened.Row.ID)
	}

	var payload gate.ClosingPayload
	if err := json.Unmarshal([]byte(closing.Payload), &payload); err != nil {
		t.Fatalf("unmarshalling the closing payload: %v", err)
	}
	if payload.Verdict != string(gate.VerdictReject) {
		t.Errorf("the closing says verdict %q, want %q", payload.Verdict, gate.VerdictReject)
	}
	if payload.Feedback != feedback {
		t.Errorf("the closing says feedback %q, want %q", payload.Feedback, feedback)
	}

	if err := decisionlog.Verify(ctx, pool); err != nil {
		t.Fatalf("Verify after fire and reject: %v", err)
	}
}

// TestAVerdictOutsideTheTwoActionsIsRefused: Merge to master has Approve and
// Reject with feedback, and no third action.
func TestAVerdictOutsideTheTwoActionsIsRefused(t *testing.T) {
	ctx, _, g := newGate(t)

	opened, err := g.Fire(ctx, firing)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if _, err := g.Decide(ctx, opened, owner, gate.Verdict("hold"), "held"); !errors.Is(err, gate.ErrVerdictUnknown) {
		t.Fatalf("Decide(hold) = %v, want ErrVerdictUnknown", err)
	}
}

// TestAnIncompleteFiringIsRefused: Merge to master always has an item, a
// build, and an artifact version, so a firing missing one is a caller's
// defect and appends nothing.
func TestAnIncompleteFiringIsRefused(t *testing.T) {
	ctx, pool, g := newGate(t)

	incomplete := firing
	incomplete.BuildID = ""
	if _, err := g.Fire(ctx, incomplete); !errors.Is(err, gate.ErrFiringIncomplete) {
		t.Fatalf("Fire(no build) = %v, want ErrFiringIncomplete", err)
	}

	rows, err := decisionlog.Read(ctx, pool)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("the log holds %d rows after the refused firing, want none", len(rows))
	}
}

// TestTheStubNamesEveryFactorUnavailable: the stub's vector has a factor from
// each of the design's three groups, every one marked unavailable with the
// reason, resolved to the value that puts a human at the gate — and its
// answer is always that a human decides.
func TestTheStubNamesEveryFactorUnavailable(t *testing.T) {
	assessment, err := gate.Stub{}.Assess(t.Context(), gate.Change{})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if assessment.Version != gate.StubVersion {
		t.Errorf("the stub names version %q, want %q", assessment.Version, gate.StubVersion)
	}
	if !assessment.HumanDecides {
		t.Errorf("the stub's answer is not that a human decides")
	}
	if assessment.Number != gate.StubValue {
		t.Errorf("the stub's number is %q, want %q", assessment.Number, gate.StubValue)
	}
	if len(assessment.Vector) == 0 {
		t.Fatal("the stub's vector is empty")
	}

	groups := map[string]bool{}
	for _, factor := range assessment.Vector {
		if factor.Unavailable != gate.StubUnavailable {
			t.Errorf("factor %q is unavailable %q, want %q", factor.Name, factor.Unavailable, gate.StubUnavailable)
		}
		if factor.Value != gate.StubValue {
			t.Errorf("factor %q resolves to %q, want %q", factor.Name, factor.Value, gate.StubValue)
		}
		group, _, found := strings.Cut(factor.Name, ".")
		if !found {
			t.Errorf("factor %q names no group", factor.Name)
		}
		groups[group] = true
	}
	for _, group := range []string{"change", "authorship", "context"} {
		if !groups[group] {
			t.Errorf("the vector names no factor of the %s group", group)
		}
	}
}
