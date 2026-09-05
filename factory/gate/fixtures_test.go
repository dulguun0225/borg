// The database tests of this package are in gate_test, opening the pool
// through package postgres. fakeScore and fakePolicy are the fakes every test
// in this package builds a firing over, and newGate is the schema-scoped pool.
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package gate_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/criterion"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/lease"
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
func newGate(t *testing.T, s gate.Score, p *fakePolicy) (context.Context, *pgxpool.Pool, lease.Token, *gate.Gate) {
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
	for n, statement := range lease.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying lease statement %d: %v", n+1, err)
		}
	}
	for n, statement := range decisionlog.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying decisionlog statement %d: %v", n+1, err)
		}
	}
	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("acquiring the lease: %v", err)
	}
	return ctx, pool, token, gate.New(decisionlog.NewWriter(pool, token), s, p, gate.NoDriftDetector{})
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

var owner = record.Actor{Kind: record.KindHuman, Key: "person:owner", Basis: record.BasisClaimed}

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
