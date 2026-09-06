// The database tests of this package are in gate_test, opening the pool through
// package postgres: a firing reads the artifact store, the People declaration,
// the halt, the environment's targets, the area's hazard severity, the service's
// four deployer fields and the item's per-stage count, so the whole schema is
// applied rather than the log's alone.
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package gate_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/service"
)

const (
	testPolicyVersion = "pv_00000000000000000000000000000001"
	testScoreVersion  = "scv_0000000000000000000000000000001"
)

var owner = record.Actor{Kind: record.KindHuman, Key: "person:owner", Basis: record.BasisClaimed}

// fakeScore answers with one assessment and records what it was asked, so a test
// can assert that the gate handed the score what the firing knew.
type fakeScore struct {
	assessment score.Assessment
	asked      score.Change
	err        error

	// selection is what HoldOut answers, and the asked* fields are what it was
	// handed: the item, the rate in force, whether the score would have gated
	// and whether a safeguard did (askedHoldOut, in that order), and what the
	// vector resolved.
	selection     score.Selection
	selectionErr  error
	askedItemID   string
	askedRate     float64
	askedHoldOut  [2]bool
	askedResolved []score.Resolution
}

func (f *fakeScore) Assess(_ context.Context, c score.Change) (score.Assessment, error) {
	f.asked = c
	assessment := f.assessment
	assessment.FactorSet = c.FactorSet
	return assessment, f.err
}

func (f *fakeScore) HoldOut(_ context.Context, itemID string, rate float64,
	wouldGate, bySafeguard bool, resolved []score.Resolution) (score.Selection, error) {
	f.askedItemID, f.askedRate = itemID, rate
	f.askedHoldOut = [2]bool{wouldGate, bySafeguard}
	f.askedResolved = resolved
	selection := f.selection
	selection.RateInForce = rate
	return selection, f.selectionErr
}

func (f *fakeScore) Version() score.Version {
	return score.Version{ID: testScoreVersion, FormulaVersion: score.FormulaVersion}
}

// fakePolicy answers with one applied policy and one number for every other
// parameter, and records the subjects each read was performed against.
type fakePolicy struct {
	applied policy.Applied
	asked   policy.Subjects
	// heldOutRate, reviewRate, attemptLimit and exposureBound are what the four
	// other reads answer, and askedDuty is the duty the review sample rate was
	// read for.
	heldOutRate   float64
	reviewRate    float64
	attemptLimit  float64
	exposureBound float64
	askedDuty     int
}

func (f *fakePolicy) AtGate(_ context.Context, _ record.Actor, s policy.Subjects) (policy.Applied, error) {
	f.asked = s
	return f.applied, nil
}

func (f *fakePolicy) HeldOutSampleRate(_ context.Context, _ policy.Subjects) (policy.Effective, error) {
	return policy.Effective{Number: f.heldOutRate}, nil
}

func (f *fakePolicy) ReviewSampleRate(_ context.Context, s policy.Subjects) (policy.Effective, error) {
	f.askedDuty = s.Duty
	return policy.Effective{Number: f.reviewRate}, nil
}

func (f *fakePolicy) AttemptLimit(_ context.Context, _ policy.Subjects) (policy.Effective, error) {
	return policy.Effective{Number: f.attemptLimit}, nil
}

func (f *fakePolicy) ExposureBound(_ context.Context, _ policy.Subjects) (policy.Effective, error) {
	return policy.Effective{Number: f.exposureBound}, nil
}

// fakeHolds answers with one set of holds, so a test can stand a condition up
// and take it away again without writing the records each one reads.
type fakeHolds struct {
	standing []string
	asked    gate.Subjects
	err      error
}

func (f *fakeHolds) Standing(_ context.Context, s gate.Subjects) ([]string, error) {
	f.asked = s
	return f.standing, f.err
}

// fakeDrift answers with one mismatch.
type fakeDrift struct {
	found bool
	why   string
}

func (f fakeDrift) Mismatch(context.Context, string) (bool, string, error) {
	return f.found, f.why, nil
}

// fakeNotifier records the one call the gate makes on it.
type fakeNotifier struct {
	acknowledged []string
}

func (f *fakeNotifier) Acknowledged(_ context.Context, openID string, _ record.Actor) error {
	f.acknowledged = append(f.acknowledged, openID)
	return nil
}

// refined is the intent state reader most tests are composed with: every item's
// intent is refined, which stops nothing.
func refined(context.Context, string) (intent.State, error) { return intent.StateRefined, nil }

// assessed is an assessment with a number and nothing resolved, which is what
// most of these tests vary.
func assessed(number float64) score.Assessment {
	return score.Assessment{
		Version:          testScoreVersion,
		FormulaVersion:   score.FormulaVersion,
		Number:           number,
		Likelihood:       0.4,
		Impact:           0.5,
		DiscountedImpact: 0.3,
		Vector: []score.Factor{
			{Name: "change.size", Group: score.GroupChange, Term: score.TermLikelihood, Level: 0.1, Weight: 0.3, Reading: "20 lines changed"},
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

// schemaPool gives a test a schema of its own with the whole factory's DDL
// applied inside it and a lease acquired over it, dropped when the test ends so
// a rerun on a database a previous run left dirty starts clean.
func schemaPool(t *testing.T) (context.Context, *pgxpool.Pool, lease.Token) {
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
	if err := postgres.Apply(ctx, pool); err != nil {
		t.Fatalf("applying the schema: %v", err)
	}
	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("acquiring the lease: %v", err)
	}
	return ctx, pool, token
}

// newGate gives a test a schema of its own and a gate over the given score and
// policy, every other component left at its default: no holds ever stand, no
// mismatch is ever found, every item's intent is refined, and nothing is
// notified. A test that needs one of those varied uses [newGateWith].
func newGate(t *testing.T, s gate.Score, p gate.Policy) (context.Context, *pgxpool.Pool, lease.Token, *gate.Gate) {
	t.Helper()
	ctx, pool, token := schemaPool(t)
	g := gate.New(gate.Composition{
		Pool: pool, Token: token, Log: decisionlog.NewWriter(pool, token),
		Score: s, Policy: p, IntentState: refined,
	})
	return ctx, pool, token, g
}

// newGateWith is [newGate] with the composition open to a test's own change,
// for a test that has to vary one of the components newGate defaults.
func newGateWith(t *testing.T, s gate.Score, p gate.Policy, change func(*gate.Composition)) (context.Context, *pgxpool.Pool, lease.Token, *gate.Gate) {
	t.Helper()
	ctx, pool, token := schemaPool(t)
	composition := gate.Composition{
		Pool: pool, Token: token, Log: decisionlog.NewWriter(pool, token),
		Score: s, Policy: p, IntentState: refined,
	}
	if change != nil {
		change(&composition)
	}
	return ctx, pool, token, gate.New(composition)
}

// rowByID is the row of rows whose id is id, and a fatal failure where none
// matches. A row's position in the log is not its part: [Gate.Fire]'s own
// check that nothing is already pending reads the log first, through
// [decisionlog.Reader.Pending], which appends a read event of its own ahead of
// the open event the firing goes on to append.
func rowByID(t *testing.T, rows []decisionlog.Row, id string) decisionlog.Row {
	t.Helper()
	for _, r := range rows {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no row in the log has id %q", id)
	return decisionlog.Row{}
}

// decisionPart is every row of rows in the given part, in log order.
func decisionPart(rows []decisionlog.Row, part decisionlog.Part) []decisionlog.Row {
	var found []decisionlog.Row
	for _, r := range rows {
		if r.Shape == decisionlog.ShapeDecision && r.Part == part {
			found = append(found, r)
		}
	}
	return found
}

// lastOpeningPayload reads the opening payload of the log's latest opening —
// the one a test that fired once and read nothing else since wants.
func lastOpeningPayload(t *testing.T, ctx context.Context, pool *pgxpool.Pool, token lease.Token) gate.OpeningPayload {
	t.Helper()
	rows, err := decisionlog.NewReader(pool, token).Read(ctx, owner)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	opens := decisionPart(rows, decisionlog.PartOpen)
	if len(opens) == 0 {
		t.Fatal("the log holds no opening")
	}
	var payload gate.OpeningPayload
	if err := json.Unmarshal([]byte(opens[len(opens)-1].Payload), &payload); err != nil {
		t.Fatalf("unmarshalling the opening payload: %v", err)
	}
	return payload
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

// mergeFiring is one Merge to master firing, complete: the item, the build, the
// records the score and the policy are read against, and two criteria results.
// The merge row is an event gate, so it names no artifact version.
var mergeFiring = gate.Firing{
	Row:             gate.MergeToMaster,
	ItemID:          "it_0000000000000000000000000000000a",
	BuildID:         "bl_0000000000000000000000000000000a",
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

// deployFiring is the same change at the production deploy row, which offers
// hold and picks the strategy. The merge row names no artifact, so the deploy
// row does not either. It writes a real service and a real environment,
// because that row reads both: the deployer's four fields off the service, and
// whether every target serves a share off the environment. The area is left
// unnamed, which is what keeps the strategy from reading one that does not
// exist.
func deployFiring(t *testing.T, ctx context.Context, pool *pgxpool.Pool, token lease.Token) gate.Firing {
	t.Helper()
	f := mergeFiring
	f.Row = gate.DeployToProduction
	f.ReleaseID = "rel_000000000000000000000000000000a"
	f.AreaID = ""
	f.ServiceID = deployableService(t, ctx, pool, token)
	f.EnvironmentID = deployableEnvironment(t, ctx, pool, token)
	return f
}

// deployableService writes a service with the deployer's four fields already
// adopted, so a deploy to production firing over it does not stop on a service
// missing one of them.
func deployableService(t *testing.T, ctx context.Context, pool *pgxpool.Pool, token lease.Token) string {
	t.Helper()
	s, err := service.NewWriter(pool, token).Create(ctx, owner,
		"svc-under-test", "git@example.com/repo.git", "prj_00000000000000000000000000000a")
	if err != nil {
		t.Fatalf("creating the service a deploy to production firing reads: %v", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning the adoption of %s: %v", s.ID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := service.Adopt(ctx, tx, token, owner, s.ID, service.Reachability{
		TargetReached: true, InstancesReplaceable: true, RollbackPathPresent: true, EmissionReadable: true,
	}); err != nil {
		t.Fatalf("adopting %s: %v", s.ID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("committing the adoption of %s: %v", s.ID, err)
	}
	return s.ID
}

// deployableEnvironment writes a production environment whose one target
// serves a share, which is what a deploy to production firing reads to pick the
// rollout strategy.
func deployableEnvironment(t *testing.T, ctx context.Context, pool *pgxpool.Pool, token lease.Token) string {
	t.Helper()
	e, err := environment.NewWriter(pool, token).Create(ctx, owner, environment.Spec{
		Kind:       environment.KindProduction,
		ProjectID:  "prj_00000000000000000000000000000a",
		Name:       environment.ProductionName,
		Targets:    []environment.Target{{Address: "/srv/targets/one", ServesAShare: true}},
		Credential: secretref.MustNew("deploy.local"),
		Platform: environment.Platform{
			Name: "local", Credential: secretref.MustNew("platform.local"), CanComposeOnDemand: true,
		},
	})
	if err != nil {
		t.Fatalf("creating the environment a deploy to production firing reads: %v", err)
	}
	return e.ID
}

// candidateFiring is the same change at the candidate deploy row, where the
// criteria are not yet decided.
func candidateFiring() gate.Firing {
	f := mergeFiring
	f.Row = gate.DeployToCandidateEnvironment
	f.Criteria = nil
	return f
}
