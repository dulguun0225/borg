// pass is exercised directly here rather than through passCommand, because it
// is the whole of the comparison and passCommand is only the flag parsing and
// the two stores' plumbing around it. It is an internal test — package main —
// because pass is unexported.
//
// Two stores are opened for every test: the factory's own, on a schema of its
// own with the whole factory schema applied through postgres.Apply, exactly
// as cmd/factory/main_test.go's newPath does — and the drift detector's own, on a
// schema of its own with its own schema applied through driftdetector.Apply,
// exactly as cmd/factory/watch_test.go's newDriftDetectorStore does. Neither
// test skips when its database is unreachable: the milestone is demonstrated
// by them running, so an unreachable database fails the run.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/project"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/targetseam"
	"github.com/dulguun0225/borg/factory/window"
)

// testActor is who these tests write the factory's records as, a component
// like any other actor here — nothing in these tests is a human's act.
var testActor = record.Actor{Kind: record.KindComponent, Key: "test"}

// testServiceName is the one service these tests write.
const testServiceName = "demo"

// newStores gives a test the two pools [pass] reads and writes, and the
// fencing token every writer these tests construct on the factory's pool
// carries.
func newStores(t *testing.T) (context.Context, stores, lease.Token) {
	t.Helper()
	ctx := t.Context()
	factory, token := newFactoryStore(t, ctx)
	return ctx, stores{factory: factory, own: newDriftDetectorStore(t, ctx)}, token
}

// newFactoryStore is a schema of its own with the whole factory schema
// applied, the way cmd/factory/main_test.go's newPath opens one, with a lease
// acquired the same way that test's fixtures acquire one.
func newFactoryStore(t *testing.T, ctx context.Context) (*pgxpool.Pool, lease.Token) {
	t.Helper()
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "factory_" + hex.EncodeToString(suffix[:])

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
		t.Fatalf("applying the factory's schema: %v", err)
	}
	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("acquiring the lease: %v", err)
	}
	return pool, token
}

// newDriftDetectorStore is the drift detector's own store for one test: a schema of
// its own, its own schema applied by its own applier, and nothing of the
// factory's in it — the way cmd/factory/watch_test.go's newDriftDetectorStore
// opens one.
func newDriftDetectorStore(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the drift detector's schema: %v", err)
	}
	schema := "driftdetector_" + hex.EncodeToString(suffix[:])

	pool, err := driftdetector.Open(ctx, inSchema(t, driftdetector.DefaultURL, schema))
	if err != nil {
		t.Fatalf("the drift detector's store is not reachable, and these tests do not skip: %v", err)
	}
	t.Cleanup(func() {
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
	if err := driftdetector.Apply(ctx, pool); err != nil {
		t.Fatalf("applying the drift detector's schema: %v", err)
	}
	return pool
}

// inSchema points a connection URL at one schema and nothing else, so every
// unqualified name in the DDL and in the writers' statements resolves there.
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

// setUp gives a test a production environment naming dir as its one target
// and a service, both written the way an owner and decomposition would write them,
// and the credential every operation on the target requires.
func setUp(ctx context.Context, t *testing.T, pool *pgxpool.Pool, token lease.Token, dir string) (environment.Environment, service.Service, secretref.Ref) {
	t.Helper()
	credential := secretref.MustNew("deploy.local")
	prj, err := project.NewWriter(pool, token).Create(ctx, testActor, "default")
	if err != nil {
		t.Fatalf("creating the project: %v", err)
	}
	env, err := environment.NewWriter(pool, token).Create(ctx, testActor, environment.Spec{
		Kind:       environment.KindProduction,
		ProjectID:  prj.ID,
		Name:       environment.ProductionName,
		Targets:    []environment.Target{{Address: dir}},
		Credential: credential,
		Platform:   environment.Platform{Name: "local", Credential: credential, CanComposeOnDemand: true},
	})
	if err != nil {
		t.Fatalf("creating the production environment: %v", err)
	}
	svc, err := service.NewWriter(pool, token).Create(ctx, testActor, testServiceName, "github.com/example/demo", prj.ID)
	if err != nil {
		t.Fatalf("creating the service: %v", err)
	}
	return env, svc, credential
}

// shipRelease writes a build, mints a release of it, and completes a deploy of
// that release into env — so [deploy.Current] names it, the way a production
// deploy the factory performed would.
func shipRelease(ctx context.Context, t *testing.T, pool *pgxpool.Pool, token lease.Token, svc service.Service, env environment.Environment, commitHash string) deploy.Deploy {
	t.Helper()
	d := startRelease(ctx, t, pool, token, svc, env, commitHash)
	if err := deploy.NewWriter(pool, token).Complete(ctx, d.ID); err != nil {
		t.Fatalf("completing the deploy: %v", err)
	}
	return d
}

// startRelease writes a build, mints a release of it, and starts — but does
// not complete — a deploy of that release into env: what [deploy.Current]
// does not yet name, and what a test opens a analysis window over.
func startRelease(ctx context.Context, t *testing.T, pool *pgxpool.Pool, token lease.Token, svc service.Service, env environment.Environment, commitHash string) deploy.Deploy {
	t.Helper()
	itemID := record.NewID("it")
	b, err := build.NewWriter(pool, token).Create(ctx, testActor, build.Draft{
		ItemID:         itemID,
		ServiceID:      svc.ID,
		CommitHash:     commitHash,
		ArtifactDigest: "sha256:" + commitHash,
	})
	if err != nil {
		t.Fatalf("creating the build: %v", err)
	}
	rel, err := release.NewWriter(pool, token).Mint(ctx, testActor, svc.ID, b.ID, itemID)
	if err != nil {
		t.Fatalf("minting the release: %v", err)
	}
	d, err := deploy.NewWriter(pool, token).Start(ctx, testActor, svc.ID, env.ID, deploy.OfRelease(rel.ID, b.ID))
	if err != nil {
		t.Fatalf("starting the deploy: %v", err)
	}
	return d
}

// recordRunning makes localtarget.New(dir) report build running for service,
// by writing the file it reads rather than starting a real process: this
// test's own pid is what keeps it reporting alive, and this process outlives
// the call to pass.
func recordRunning(t *testing.T, dir, svc, build string) {
	t.Helper()
	content := build + " " + strconv.Itoa(os.Getpid())
	if err := os.WriteFile(localtarget.RunningFile(dir, svc), []byte(content), 0o644); err != nil {
		t.Fatalf("recording what %s runs in %s: %v", svc, dir, err)
	}
}

// erroringTarget is a [targetseam.Target] whose ReadRunning always fails —
// the network blip a pass is written to shrug off rather than a disagreement.
type erroringTarget struct{ err error }

func (erroringTarget) Deploy(context.Context, targetseam.Deployment) error { return nil }
func (erroringTarget) Stop(context.Context, string, secretref.Ref) error   { return nil }
func (e erroringTarget) ReadRunning(context.Context, string, secretref.Ref) (targetseam.Running, error) {
	return targetseam.Running{}, e.err
}

func TestAPassWhereTheTargetAgreesRaisesNoMismatch(t *testing.T) {
	ctx, s, token := newStores(t)
	dir := t.TempDir()
	env, svc, credential := setUp(ctx, t, s.factory, token, dir)
	current := shipRelease(ctx, t, s.factory, token, svc, env, "c1")
	recordRunning(t, dir, testServiceName, current.BuildID)

	out := &strings.Builder{}
	targetAt := func(dir string) targetseam.Target { return localtarget.New(dir) }
	if err := pass(ctx, s, out, credential, targetAt); err != nil {
		t.Fatalf("pass: %v", err)
	}

	if !strings.Contains(out.String(), "agrees") {
		t.Errorf("the report does not say the target agrees:\n%s", out)
	}
	held, why, err := driftdetector.NewStore(s.own).Mismatch(ctx, svc.ID)
	if err != nil || held {
		t.Errorf("Mismatch = %v %q, %v; a target running the recorded build raises none", held, why, err)
	}
	checks, err := driftdetector.LastChecks(ctx, s.own, svc.ID)
	if err != nil || len(checks) != 1 || !checks[0].Agreed {
		t.Errorf("LastChecks = %+v, %v, want one check agreeing", checks, err)
	}
}

// TestAPassWhereTheTargetDisagreesRaisesAMismatchTheReportNames is the row
// the production deploy gate holds on: raised by the pass, and named in the
// report a human at the drift detector reads.
func TestAPassWhereTheTargetDisagreesRaisesAMismatchTheReportNames(t *testing.T) {
	ctx, s, token := newStores(t)
	dir := t.TempDir()
	env, svc, credential := setUp(ctx, t, s.factory, token, dir)
	current := shipRelease(ctx, t, s.factory, token, svc, env, "c1")
	recordRunning(t, dir, testServiceName, "bl_somebodyelses")

	out := &strings.Builder{}
	targetAt := func(dir string) targetseam.Target { return localtarget.New(dir) }
	if err := pass(ctx, s, out, credential, targetAt); err != nil {
		t.Fatalf("pass: %v", err)
	}

	if !strings.Contains(out.String(), "MISMATCH") {
		t.Errorf("the report does not name the mismatch:\n%s", out)
	}
	uncleared, err := driftdetector.Uncleared(ctx, s.own, svc.ID)
	if err != nil || len(uncleared) != 1 {
		t.Fatalf("Uncleared = %+v, %v, want the one mismatch just raised", uncleared, err)
	}
	m := uncleared[0]
	if m.RunningBuild != "bl_somebodyelses" || m.RecordedBuildID != current.BuildID {
		t.Errorf("the mismatch reads running %q recorded %q, want %q and %q",
			m.RunningBuild, m.RecordedBuildID, "bl_somebodyelses", current.BuildID)
	}
}

func TestATargetThatErrorsOnReadRunningWritesAnUnreachedLastCheckAndRaisesNoMismatch(t *testing.T) {
	ctx, s, token := newStores(t)
	dir := t.TempDir()
	env, svc, credential := setUp(ctx, t, s.factory, token, dir)
	shipRelease(ctx, t, s.factory, token, svc, env, "c1")

	failure := errors.New("dial tcp: connection refused")
	targetAt := func(string) targetseam.Target { return erroringTarget{err: failure} }
	out := &strings.Builder{}
	if err := pass(ctx, s, out, credential, targetAt); err != nil {
		t.Fatalf("pass: %v", err)
	}

	if !strings.Contains(out.String(), "could not be reached") {
		t.Errorf("the report does not say the target could not be reached:\n%s", out)
	}
	checks, err := driftdetector.LastChecks(ctx, s.own, svc.ID)
	if err != nil {
		t.Fatalf("LastChecks: %v", err)
	}
	if len(checks) != 1 || checks[0].Reached || checks[0].Why != failure.Error() {
		t.Errorf("LastChecks = %+v, want one unreached check naming %q", checks, failure)
	}
	held, _, err := driftdetector.NewStore(s.own).Mismatch(ctx, svc.ID)
	if err != nil || held {
		t.Errorf("Mismatch = %v, %v; failing to reach a target is not a mismatch", held, err)
	}
}

func TestNoProductionEnvironmentIsNothingToCheckAndWritesNothing(t *testing.T) {
	ctx, s, token := newStores(t)
	credential := secretref.MustNew("deploy.local")
	prj, err := project.NewWriter(s.factory, token).Create(ctx, testActor, "default")
	if err != nil {
		t.Fatalf("creating the project: %v", err)
	}
	if _, err := service.NewWriter(s.factory, token).Create(ctx, testActor, testServiceName, "github.com/example/demo", prj.ID); err != nil {
		t.Fatalf("creating the service: %v", err)
	}
	calls := 0
	targetAt := func(dir string) targetseam.Target { calls++; return localtarget.New(dir) }

	out := &strings.Builder{}
	if err := pass(ctx, s, out, credential, targetAt); err != nil {
		t.Fatalf("pass: %v", err)
	}
	if !strings.Contains(out.String(), "no production environment") {
		t.Errorf("the report does not say there is no production environment:\n%s", out)
	}
	if calls != 0 {
		t.Errorf("the pass reached %d targets with no production environment recorded, want none", calls)
	}
	checks, err := driftdetector.LastChecks(ctx, s.own, "")
	if err != nil || len(checks) != 0 {
		t.Errorf("LastChecks = %+v, %v, want nothing written", checks, err)
	}
}

func TestNoServicesIsNothingToCheckAndWritesNothing(t *testing.T) {
	ctx, s, token := newStores(t)
	dir := t.TempDir()
	credential := secretref.MustNew("deploy.local")
	prj, err := project.NewWriter(s.factory, token).Create(ctx, testActor, "default")
	if err != nil {
		t.Fatalf("creating the project: %v", err)
	}
	if _, err := environment.NewWriter(s.factory, token).Create(ctx, testActor, environment.Spec{
		Kind:       environment.KindProduction,
		ProjectID:  prj.ID,
		Name:       environment.ProductionName,
		Targets:    []environment.Target{{Address: dir}},
		Credential: credential,
		Platform:   environment.Platform{Name: "local", Credential: credential, CanComposeOnDemand: true},
	}); err != nil {
		t.Fatalf("creating the production environment: %v", err)
	}
	calls := 0
	targetAt := func(dir string) targetseam.Target { calls++; return localtarget.New(dir) }

	out := &strings.Builder{}
	if err := pass(ctx, s, out, credential, targetAt); err != nil {
		t.Fatalf("pass: %v", err)
	}
	if !strings.Contains(out.String(), "no services") {
		t.Errorf("the report does not say there are no services:\n%s", out)
	}
	if calls != 0 {
		t.Errorf("the pass reached %d targets with no service recorded, want none", calls)
	}
	checks, err := driftdetector.LastChecks(ctx, s.own, "")
	if err != nil || len(checks) != 0 {
		t.Errorf("LastChecks = %+v, %v, want nothing written", checks, err)
	}
}

// factoryCounts is the row count of every table the independence rule
// covers: deploy, release, and the decision log — what a gate, a mint, or a
// page would have written had the pass been evidence about the software
// rather than a read of it.
type factoryCounts struct{ deploy, release, decisionLog int }

func countFactoryRows(ctx context.Context, t *testing.T, pool *pgxpool.Pool) factoryCounts {
	t.Helper()
	var c factoryCounts
	for table, dest := range map[string]*int{
		deploy.Table:      &c.deploy,
		release.Table:     &c.release,
		decisionlog.Table: &c.decisionLog,
	} {
		if err := pool.QueryRow(ctx, `select count(*) from `+table).Scan(dest); err != nil {
			t.Fatalf("counting %s: %v", table, err)
		}
	}
	return c
}

// TestThePassWritesNothingIntoTheFactorysStore is the independence rule
// checked rather than asserted in a comment: nothing the drift detector writes is
// evidence about the software, so a pass that raises a mismatch still leaves
// the factory's own tables exactly as they were.
func TestThePassWritesNothingIntoTheFactorysStore(t *testing.T) {
	ctx, s, token := newStores(t)
	dir := t.TempDir()
	env, svc, credential := setUp(ctx, t, s.factory, token, dir)
	shipRelease(ctx, t, s.factory, token, svc, env, "c1")
	recordRunning(t, dir, testServiceName, "bl_somebodyelses")

	before := countFactoryRows(ctx, t, s.factory)
	targetAt := func(dir string) targetseam.Target { return localtarget.New(dir) }
	if err := pass(ctx, s, &strings.Builder{}, credential, targetAt); err != nil {
		t.Fatalf("pass: %v", err)
	}
	after := countFactoryRows(ctx, t, s.factory)
	if before != after {
		t.Errorf("the factory's row counts moved from %+v to %+v", before, after)
	}
}

// TestAnOpenWindowExcusesABuildRunningBesideTheCurrentRelease is the exception
// [excusedBuilds] reads: a build the release under watch names is a mismatch
// only where no open window accounts for it.
func TestAnOpenWindowExcusesABuildRunningBesideTheCurrentRelease(t *testing.T) {
	ctx, s, token := newStores(t)
	dir := t.TempDir()
	env, svc, credential := setUp(ctx, t, s.factory, token, dir)
	shipRelease(ctx, t, s.factory, token, svc, env, "c1")
	rolling := startRelease(ctx, t, s.factory, token, svc, env, "c2")

	if _, err := window.NewWriter(s.factory, token).Open(ctx, testActor, window.OpenEvent{
		DeployID:        rolling.ID,
		ReleaseID:       rolling.ReleaseID,
		ServiceID:       svc.ID,
		PassedAvailable: true,
		Size:            0.1,
		Confidence:      0.95,
		CapSeconds:      3600,
		Formula:         "wilson",
		PolicyVersion:   "pv_1",
		ScoreVersion:    "sv_1",
	}); err != nil {
		t.Fatalf("opening the window: %v", err)
	}
	recordRunning(t, dir, testServiceName, rolling.BuildID)

	out := &strings.Builder{}
	targetAt := func(dir string) targetseam.Target { return localtarget.New(dir) }
	if err := pass(ctx, s, out, credential, targetAt); err != nil {
		t.Fatalf("pass: %v", err)
	}

	if !strings.Contains(out.String(), "analysis window accounts for") {
		t.Errorf("the report does not say the window excuses it:\n%s", out)
	}
	held, why, err := driftdetector.NewStore(s.own).Mismatch(ctx, svc.ID)
	if err != nil || held {
		t.Errorf("Mismatch = %v %q, %v; a build an open window accounts for is excused", held, why, err)
	}
}
