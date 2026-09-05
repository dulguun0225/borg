// The database tests of this package are in deploy_test and open the pool
// through package postgres, the way decisionlog's do; deps.txt records the
// test edge. They apply this package's DDL themselves rather than calling
// postgres.Apply, which does not know this package until integration wires it
// in. The target the rollout tests reach is [targetseam.NewFake]; localtarget
// is where a real process runs, in that package's own tests.
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package deploy_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// newTable gives a test a schema of its own, this package's DDL applied
// inside it, and a writer over it. The schema is dropped when the test ends,
// so a rerun on a database a previous run left dirty starts clean.
func newTable(t *testing.T) (context.Context, *pgxpool.Pool, *deploy.Writer) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m1_" + hex.EncodeToString(suffix[:])

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
			t.Fatalf("applying the lease schema statement %d: %v", n+1, err)
		}
	}
	for n, statement := range deploy.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying statement %d: %v", n+1, err)
		}
	}
	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("acquiring the lease: %v", err)
	}
	return ctx, pool, deploy.NewWriter(pool, token)
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

var deployer = record.Actor{Kind: record.KindComponent, Key: "deploy"}

// productionID stands for production's environment record. The deploy record
// names an environment by the record's id from M2 on, and there are no foreign
// keys between record tables, so this test names one it never creates.
const productionID = "env_000000000000000000000000000000a"

// storedStatus is the status column as the store has it, read raw because the
// package exposes no Get — a deploy is read through [deploy.Current], and a
// started one is exactly what Current does not name.
func storedStatus(ctx context.Context, t *testing.T, pool *pgxpool.Pool, id string) deploy.Status {
	t.Helper()
	var status string
	if err := pool.QueryRow(ctx, `select status from deploy where id = $1`, id).Scan(&status); err != nil {
		t.Fatalf("reading the status of %s: %v", id, err)
	}
	return deploy.Status(status)
}

func TestTheRecordAdvancesInPlace(t *testing.T) {
	ctx, pool, w := newTable(t)

	d, err := w.Start(ctx, deployer, record.NewID("svc"), productionID, deploy.OfRelease(record.NewID("rel"), record.NewID("bld")))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if d.Status != deploy.StatusStarted || d.Strategy != deploy.StrategyWithoutControl {
		t.Errorf("Start returned status %q, strategy %q; want started, without_control", d.Status, d.Strategy)
	}
	if _, err := time.Parse(record.TimeLayout, d.At); err != nil {
		t.Errorf("the record has timestamp %q: %v", d.At, err)
	}
	if got := storedStatus(ctx, t, pool, d.ID); got != deploy.StatusStarted {
		t.Errorf("the store has status %q, want started", got)
	}

	var count int
	if err := w.Complete(ctx, d.ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got := storedStatus(ctx, t, pool, d.ID); got != deploy.StatusComplete {
		t.Errorf("the store has status %q, want complete", got)
	}
	if err := pool.QueryRow(ctx, `select count(*) from deploy`).Scan(&count); err != nil {
		t.Fatalf("counting the records: %v", err)
	}
	if count != 1 {
		t.Errorf("the table holds %d records, want 1 — the record advances in place", count)
	}

	if err := w.Complete(ctx, d.ID); !errors.Is(err, deploy.ErrNotStarted) {
		t.Errorf("a second Complete = %v, want %v", err, deploy.ErrNotStarted)
	}
	if err := w.Complete(ctx, "dep_00000000000000000000000000000000"); !errors.Is(err, deploy.ErrNotFound) {
		t.Errorf("Complete of nothing = %v, want %v", err, deploy.ErrNotFound)
	}
}

// TestCurrentIsWhatIsRunningNotWhatIsNewest walks the distinction the record
// exists for: a started deploy does not change the answer, and a completed one
// does.
func TestCurrentIsWhatIsRunningNotWhatIsNewest(t *testing.T) {
	ctx, pool, w := newTable(t)
	serviceID := record.NewID("svc")

	if _, running, err := deploy.Current(ctx, pool, serviceID, productionID); err != nil || running {
		t.Fatalf("Current = running %v, err %v; want nothing running in an empty table", running, err)
	}

	first, err := w.Start(ctx, deployer, serviceID, productionID, deploy.OfRelease(record.NewID("rel"), record.NewID("bld")))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, running, err := deploy.Current(ctx, pool, serviceID, productionID); err != nil || running {
		t.Fatalf("Current = running %v, err %v; a started deploy is not running", running, err)
	}

	if err := w.Complete(ctx, first.ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	current, running, err := deploy.Current(ctx, pool, serviceID, productionID)
	if err != nil || !running {
		t.Fatalf("Current = running %v, err %v; want the completed deploy", running, err)
	}
	if current.ID != first.ID {
		t.Errorf("Current names %s, want %s", current.ID, first.ID)
	}

	second, err := w.Start(ctx, deployer, serviceID, productionID, deploy.OfRelease(record.NewID("rel"), record.NewID("bld")))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	current, running, err = deploy.Current(ctx, pool, serviceID, productionID)
	if err != nil || !running {
		t.Fatalf("Current = running %v, err %v", running, err)
	}
	if current.ID != first.ID {
		t.Errorf("Current names %s while a newer deploy is only started, want still %s", current.ID, first.ID)
	}

	if err := w.Complete(ctx, second.ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	current, running, err = deploy.Current(ctx, pool, serviceID, productionID)
	if err != nil || !running {
		t.Fatalf("Current = running %v, err %v", running, err)
	}
	if current.ID != second.ID {
		t.Errorf("Current names %s, want the most recently completed, %s", current.ID, second.ID)
	}

	// Another environment of the same service answers for itself.
	if _, running, err := deploy.Current(ctx, pool, serviceID, "staging"); err != nil || running {
		t.Errorf("Current in another environment = running %v, err %v; want nothing", running, err)
	}
}

// TestTheRolloutWithoutAControlThroughTheSeam is the rollout against the seam's
// fake: the record advances to complete, the target is asked for exactly one
// named operation, and what was recorded is the credential's reference — no value
// anywhere, the fake's Call having no field that could hold one.
func TestTheRolloutWithoutAControlThroughTheSeam(t *testing.T) {
	ctx, pool, w := newTable(t)
	target := targetseam.NewFake()
	credential := secretref.MustNew("target.production")
	serviceID := record.NewID("svc")
	releaseID := record.NewID("rel")
	buildID := record.NewID("bld")

	d, err := deploy.WithoutControl(ctx, w, target, deployer, serviceID, "checkout", productionID,
		deploy.OfRelease(releaseID, buildID), credential)
	if err != nil {
		t.Fatalf("WithoutControl: %v", err)
	}
	if d.Status != deploy.StatusComplete {
		t.Errorf("WithoutControl returned status %q, want complete", d.Status)
	}
	if got := storedStatus(ctx, t, pool, d.ID); got != deploy.StatusComplete {
		t.Errorf("the store has status %q, want complete", got)
	}

	calls := target.Calls()
	if len(calls) != 1 {
		t.Fatalf("the target saw %d operations, want 1: %+v", len(calls), calls)
	}
	call := calls[0]
	if call.Op != targetseam.OpDeploy {
		t.Errorf("the operation is %q, want %q", call.Op, targetseam.OpDeploy)
	}
	if call.Service != "checkout" || call.Build != buildID {
		t.Errorf("the target was asked to deploy %q of %q, want %q of %q", call.Build, call.Service, buildID, "checkout")
	}
	if call.Credential != credential {
		t.Errorf("the call records credential reference %v, want %v", call.Credential, credential)
	}

	current, running, err := deploy.Current(ctx, pool, serviceID, productionID)
	if err != nil || !running {
		t.Fatalf("Current = running %v, err %v", running, err)
	}
	if current.ReleaseID != releaseID {
		t.Errorf("Current names release %s, want %s", current.ReleaseID, releaseID)
	}
}

// brokenTarget fails every operation, standing in for a target the rollout
// cannot reach.
type brokenTarget struct{ err error }

func (b brokenTarget) Deploy(context.Context, targetseam.Deployment) error { return b.err }
func (b brokenTarget) Stop(context.Context, string, secretref.Ref) error   { return b.err }
func (b brokenTarget) ReadRunning(context.Context, string, secretref.Ref) (targetseam.Running, error) {
	return targetseam.Running{}, b.err
}

func TestATargetErrorLeavesTheRecordStarted(t *testing.T) {
	ctx, pool, w := newTable(t)
	unreachable := errors.New("the target is unreachable")
	serviceID := record.NewID("svc")

	d, err := deploy.WithoutControl(ctx, w, brokenTarget{err: unreachable}, deployer,
		serviceID, "checkout", productionID, deploy.OfRelease(record.NewID("rel"), record.NewID("bld")),
		secretref.MustNew("target.production"))
	if !errors.Is(err, unreachable) {
		t.Fatalf("WithoutControl = %v, want the target's error", err)
	}
	if d.ID == "" {
		t.Fatal("the error path returns no record, and the started record is what a caller has to point at")
	}
	if got := storedStatus(ctx, t, pool, d.ID); got != deploy.StatusStarted {
		t.Errorf("the store has status %q, want started — the drift detector that would settle it is M4", got)
	}
	if _, running, err := deploy.Current(ctx, pool, serviceID, productionID); err != nil || running {
		t.Errorf("Current = running %v, err %v; a deploy that never completed is not running", running, err)
	}
}

func TestTheStoreRefusesWhatTheWriterRefuses(t *testing.T) {
	ctx, pool, w := newTable(t)

	if _, err := w.Start(ctx, deployer, record.NewID("svc"), "", deploy.OfRelease(record.NewID("rel"), record.NewID("bld"))); !errors.Is(err, deploy.ErrEnvironmentEmpty) {
		t.Errorf("Start with no environment = %v, want %v", err, deploy.ErrEnvironmentEmpty)
	}
	if _, err := w.Start(ctx, record.Actor{}, record.NewID("svc"), productionID, deploy.OfRelease(record.NewID("rel"), record.NewID("bld"))); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("Start with no actor = %v, want %v", err, record.ErrKindUnknown)
	}

	insert := func(environment, strategy, status string) error {
		_, err := pool.Exec(ctx, `insert into deploy (id, format_version, actor_kind, actor_key, actor_key_basis, at, service_id, environment_id, release_id, build_id, strategy, status)
			values ($1, $2, 'component', 'deploy', '', $3, $4, $5, $6, $7, $8, $9)`,
			record.NewID(deploy.IDPrefix), deploy.FormatVersion, record.Now(), record.NewID("svc"), environment,
			record.NewID("rel"), record.NewID("bld"), strategy, status)
		return err
	}
	if err := insert("", "without_control", "started"); err == nil {
		t.Error("the store accepted a deploy with no environment")
	}
	if err := insert(productionID, "with_a_control", "started"); err == nil {
		t.Error("the store accepted a strategy the CHECK does not list — with a control is M4's edit")
	}
	if err := insert(productionID, "without_control", "watching"); err == nil {
		t.Error("the store accepted a status the CHECK does not list")
	}
}

// TestAnEmptyLinkIsRefusedTwice covers this package's two link columns at one
// of them. An empty link names nothing, so it is refused by the writer and by
// the store, the way every other required field is; record's doc.go states
// what a link is checked for.
func TestAnEmptyLinkIsRefusedTwice(t *testing.T) {
	ctx, pool, w := newTable(t)

	if _, err := w.Start(ctx, deployer, "", productionID, deploy.OfRelease(record.NewID("rel"), record.NewID("bld"))); !errors.Is(err, deploy.ErrServiceIDEmpty) {
		t.Errorf("Start naming no service = %v, want %v", err, deploy.ErrServiceIDEmpty)
	}
	if _, err := w.Start(ctx, deployer, record.NewID("svc"), productionID, deploy.What{}); !errors.Is(err, deploy.ErrBuildIDEmpty) {
		t.Errorf("Start naming nothing deployed = %v, want %v", err, deploy.ErrBuildIDEmpty)
	}

	_, err := pool.Exec(ctx, `insert into deploy (id, format_version, actor_kind, actor_key, actor_key_basis, at, service_id, environment_id, release_id, build_id, strategy, status)
		values ($1, $2, 'component', 'deploy', '', $3, '', 'production', $4, $5, 'without_control', 'started')`,
		record.NewID(deploy.IDPrefix), deploy.FormatVersion, record.Now(), record.NewID("rel"), record.NewID("bld"))
	if err == nil || !strings.Contains(err.Error(), "service_id_present") {
		t.Errorf("inserting a deploy naming no service = %v, want a violation of service_id_present", err)
	}
}
