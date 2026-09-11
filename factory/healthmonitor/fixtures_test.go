// fixtures_test.go is what db_test.go and watch_test.go share: a schema of its
// own with the whole factory schema applied, the writers one test writes the
// graph through, the service the graph is about, and shipOne, which writes the
// records one release leaves behind. Splitting it out of db_test.go is what
// keeps both files under the line bound with their own tests read together.
package healthmonitor_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/targetseam"
	"github.com/dulguun0225/borg/factory/window"
)

// errorRate is the one quantity these tests carry a size for: what a window's
// exit is read against here is the arithmetic of the graph and not the reading
// itself, so one quantity is enough to satisfy every constraint an open and a
// close carry.
const errorRate = gatepolicy.QuantityErrorRate

// The target this test names records against. It does not point at a record: an
// id field is checked for being present and not for pointing at anything, which
// record's doc.go states once, and what these tests are about is the releases
// and the windows. The environment id is not a constant: incident.Raise reads
// the kind of the environment id an incident names, so [newGraph] writes a real
// production environment and every test reads its id off the graph.
const (
	theTarget = "/srv/one"
	// theServiceName is what the service record this fixture creates is called.
	// Its id is minted by the writer, so every test reads it off the graph.
	theServiceName = "under-watch"
)

var theActor = record.Actor{Kind: record.KindComponent, Key: "test", Basis: record.BasisClaimed}

// graph is the records one test writes and the writers it writes them through.
type graph struct {
	pool          *pgxpool.Pool
	token         lease.Token
	serviceID     string
	environmentID string
	builds        *build.Writer
	releases      *release.Writer
	deploys       *deploy.Writer
	windows       *window.Writer
	incidents     *incident.Writer
	items         *item.Decomposition
	monitor       *healthmonitor.HealthMonitor
}

// fakeEmission satisfies [healthmonitor.Emission] with nothing behind it: the
// tests here are the arithmetic over the graph, and none of them evaluates a
// window's series.
type fakeEmission struct{}

func (fakeEmission) Read(context.Context, healthmonitor.Reading) (healthmonitor.Series, error) {
	return healthmonitor.Series{}, nil
}
func (fakeEmission) History(context.Context, healthmonitor.History) (healthmonitor.Series, error) {
	return healthmonitor.Series{}, nil
}
func (fakeEmission) FailureRecords(context.Context, healthmonitor.Reading) ([]healthmonitor.FailureRecord, error) {
	return nil, nil
}
func (fakeEmission) Spent(context.Context, string, time.Duration) ([]healthmonitor.Spend, error) {
	return nil, nil
}
func (fakeEmission) Shape(context.Context, healthmonitor.Arm) (string, error) { return "", nil }

// newGraph gives a test a schema of its own with the whole factory schema applied.
func newGraph(t *testing.T) (context.Context, graph) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "healthmonitor_" + hex.EncodeToString(suffix[:])

	pool, err := postgres.Open(ctx, inSchema(t, postgres.URL(), schema))
	if err != nil {
		t.Fatalf("the database at %s is not reachable, and these tests do not skip: %v", postgres.URL(), err)
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
	if err := postgres.Apply(ctx, pool); err != nil {
		t.Fatalf("applying the schema: %v", err)
	}
	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("acquiring the lease: %v", err)
	}

	svc, err := service.NewWriter(pool, token).Create(ctx, theActor, theServiceName, "/repos/under-watch", "prj_test")
	if err != nil {
		t.Fatalf("creating the service the graph is about: %v", err)
	}
	env, err := environment.NewWriter(pool, token).Create(ctx, theActor, environment.Spec{
		Kind: environment.KindProduction, ProjectID: "prj_test", Name: environment.ProductionName,
		Targets:    []environment.Target{{Address: theTarget, ServesAShare: true}},
		Credential: secretref.MustNew("deploy.local"),
		Platform: environment.Platform{
			Name: "local", Credential: secretref.MustNew("platform.local"), CanComposeOnDemand: true,
		},
	})
	if err != nil {
		t.Fatalf("creating the production environment the graph is about: %v", err)
	}
	windows := window.NewWriter(pool, token)
	incidents := incident.NewWriter(pool, token)
	monitor, err := healthmonitor.New(pool, windows, incidents, nil, nil, nil, nil,
		fakeEmission{}, nil, nil, nil, nil, healthmonitor.Readings{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	return ctx, graph{
		pool:          pool,
		token:         token,
		serviceID:     svc.ID,
		environmentID: env.ID,
		builds:        build.NewWriter(pool, token),
		releases:      release.NewWriter(pool, token),
		deploys:       deploy.NewWriter(pool, token),
		windows:       windows,
		incidents:     incidents,
		items:         item.NewDecomposition(pool, token, item.NoHolds{}),
		monitor:       monitor,
	}
}

// monitorWith is a second health monitor over the same graph, composed with an
// emission that returns readings, a deployer that records what it was asked,
// and a pager that keeps the waits. The one in [graph] reads nothing and is
// what the two queries over the graph use.
func (g graph) monitorWith(t *testing.T, emission healthmonitor.Emission,
	deployer healthmonitor.Deployer, pager healthmonitor.Pager) *healthmonitor.HealthMonitor {
	t.Helper()
	return g.monitorWithMismatch(t, emission, deployer, pager, nil)
}

// monitorWithMismatch is the same with the drift detector's store answering,
// which is what stops a rollback while a mismatch stands.
func (g graph) monitorWithMismatch(t *testing.T, emission healthmonitor.Emission,
	deployer healthmonitor.Deployer, pager healthmonitor.Pager,
	mismatches healthmonitor.Mismatches) *healthmonitor.HealthMonitor {
	t.Helper()
	return g.monitorComposed(t, emission, deployer, pager, mismatches, nil, healthmonitor.Readings{})
}

// monitorComposed is the whole composition, for a test that needs what the two
// above supply nothing for: which release is a brownout, and the readings
// beside the comparison as they are in force today.
func (g graph) monitorComposed(t *testing.T, emission healthmonitor.Emission,
	deployer healthmonitor.Deployer, pager healthmonitor.Pager,
	mismatches healthmonitor.Mismatches, brownouts healthmonitor.Brownouts,
	readings healthmonitor.Readings) *healthmonitor.HealthMonitor {
	t.Helper()
	monitor, err := healthmonitor.New(g.pool, g.windows, g.incidents, nil, nil, nil, pager,
		emission, deployer, nil, mismatches, brownouts, readings)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return monitor
}

// authorTargets names the one target this service runs on, which is what every
// read of what is current in production is made against.
func (g graph) authorTargets(t *testing.T, ctx context.Context) {
	t.Helper()
	g.inTransaction(t, ctx, func(tx pgx.Tx) error {
		return service.SetTargets(ctx, tx, g.serviceID, []string{theTarget}, []string{theTarget})
	})
}

// inTransaction is the transaction package policy would append a version write
// to: every owner-authored field on the service record takes the caller's.
func (g graph) inTransaction(t *testing.T, ctx context.Context, write func(pgx.Tx) error) {
	t.Helper()
	tx, err := g.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, g.token); err != nil {
		t.Fatalf("fencing: %v", err)
	}
	if err := write(tx); err != nil {
		t.Fatalf("writing an authored field: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("committing: %v", err)
	}
}

// authorObjective writes the service level objective and its period the way
// package policy would: inside a transaction it fenced first.
func (g graph) authorObjective(t *testing.T, ctx context.Context, target, periodSeconds float64) {
	t.Helper()
	g.inTransaction(t, ctx, func(tx pgx.Tx) error {
		return service.SetObjective(ctx, tx, g.serviceID, target, periodSeconds)
	})
}

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

// watching is the service these tests read the graph as.
func (g graph) watching() healthmonitor.Watching {
	return healthmonitor.Watching{ID: g.serviceID, Name: theServiceName, EnvironmentID: g.environmentID}
}

// shipOne writes the records one release leaves behind: an item, a build, the
// release, a completed production deploy of it, and a window over that deploy
// at the exit named. An empty exit leaves the window open.
//
// It is what a run does, written directly. The point of writing it here rather
// than running the path is that a test can put the windows in an order a run
// cannot easily produce, which is exactly the case the rollback's target
// exists for.
func shipOne(t *testing.T, ctx context.Context, g graph, intentID string, exit window.Exit) release.Release {
	t.Helper()
	return shipOneWith(t, ctx, g, intentID, exit, nil)
}

// shipOneWith is [shipOne] with the opening altered before the window is
// written, which is what a test needs to put a window in a shape the fixture's
// own opening does not take: a set of operations read alone, or no size for the
// reading against the service's own recent history.
func shipOneWith(t *testing.T, ctx context.Context, g graph, intentID string, exit window.Exit,
	alter func(*window.OpenEvent)) release.Release {
	t.Helper()
	it, err := g.items.Create(ctx, theActor, item.New{
		IntentID: intentID, ServiceID: g.serviceID, Branch: "item/" + intentID,
		RequirementsAnswered: []string{"rq_" + "test"},
	}, "", "")
	if err != nil {
		t.Fatalf("decomposing the item: %v", err)
	}
	bl, err := g.builds.Create(ctx, theActor, build.Draft{
		ItemID: it.ID, ServiceID: g.serviceID, CommitHash: "commit-" + intentID, ArtifactDigest: "digest-" + intentID,
		ShippedBundleIdentity: "bundle-test",
	})
	if err != nil {
		t.Fatalf("writing the build: %v", err)
	}
	rel, err := g.releases.Mint(ctx, theActor, release.Minting{
		ServiceID: g.serviceID, BuildID: bl.ID, Commit: bl.CommitHash, ItemID: it.ID,
	})
	if err != nil {
		t.Fatalf("minting the release: %v", err)
	}
	dep, err := g.deploys.Start(ctx, theActor, deploy.Beginning{
		ServiceID: g.serviceID, EnvironmentID: g.environmentID,
		What: deploy.OfRelease(rel.ID, bl.ID), Targets: []deploy.Reaching{{Address: theTarget, KeptInstances: 1}},
	})
	if err != nil {
		t.Fatalf("starting the deploy: %v", err)
	}
	if err := g.deploys.ReachTarget(ctx, dep.ID, theTarget); err != nil {
		t.Fatalf("reaching the target: %v", err)
	}
	if err := g.deploys.CompleteTarget(ctx, dep.ID, theTarget, targetseam.ReplacementDrained); err != nil {
		t.Fatalf("completing the target: %v", err)
	}
	if err := g.deploys.Complete(ctx, dep.ID); err != nil {
		t.Fatalf("completing the deploy: %v", err)
	}
	opening := window.OpenEvent{
		DeployID: dep.ID, ReleaseID: rel.ID, BuildID: bl.ID, ServiceID: g.serviceID,
		PassedAvailable: rel.Number > 1,
		Size:            map[gatepolicy.Quantity]float64{errorRate: 0.1},
		Power:           map[gatepolicy.Quantity]float64{errorRate: 0.8},
		Confidence:      0.95, CapSeconds: 60,
		BoundaryVersion: boundary.Version, Targets: []string{theTarget},
		OwnHistorySize:         map[gatepolicy.Quantity]float64{errorRate: 0.1},
		OwnHistoryRunLength:    500,
		EmissionVersionRelease: "emission/1", PolicyVersion: "pv_test", ScoreVersion: "scv_test",
	}
	if alter != nil {
		alter(&opening)
	}
	w, err := g.windows.Open(ctx, healthmonitor.Actor, opening)
	if err != nil {
		t.Fatalf("opening the window: %v", err)
	}
	if exit != "" {
		if _, err := g.windows.Close(ctx, w.ID, exit, readFor(exit)); err != nil {
			t.Fatalf("closing the window at %s: %v", exit, err)
		}
	}
	return rel
}

// shipOneUnmeasured is [shipOne] for a release watched by a window that
// measures nothing: the deploy completes the same way, but the window it opens
// records only that and closes timed out at the open, the way a service
// missing one of the four fields the deployer populates is watched. It is what
// a rollback's target still descends to, since that query asks whether any
// window failed the release and not whether anything measured it.
func shipOneUnmeasured(t *testing.T, ctx context.Context, g graph, intentID string) release.Release {
	t.Helper()
	it, err := g.items.Create(ctx, theActor, item.New{
		IntentID: intentID, ServiceID: g.serviceID, Branch: "item/" + intentID,
		RequirementsAnswered: []string{"rq_" + "test"},
	}, "", "")
	if err != nil {
		t.Fatalf("decomposing the item: %v", err)
	}
	bl, err := g.builds.Create(ctx, theActor, build.Draft{
		ItemID: it.ID, ServiceID: g.serviceID, CommitHash: "commit-" + intentID, ArtifactDigest: "digest-" + intentID,
		ShippedBundleIdentity: "bundle-test",
	})
	if err != nil {
		t.Fatalf("writing the build: %v", err)
	}
	rel, err := g.releases.Mint(ctx, theActor, release.Minting{
		ServiceID: g.serviceID, BuildID: bl.ID, Commit: bl.CommitHash, ItemID: it.ID,
	})
	if err != nil {
		t.Fatalf("minting the release: %v", err)
	}
	dep, err := g.deploys.Start(ctx, theActor, deploy.Beginning{
		ServiceID: g.serviceID, EnvironmentID: g.environmentID,
		What: deploy.OfRelease(rel.ID, bl.ID), Targets: []deploy.Reaching{{Address: theTarget, KeptInstances: 1}},
	})
	if err != nil {
		t.Fatalf("starting the deploy: %v", err)
	}
	if err := g.deploys.ReachTarget(ctx, dep.ID, theTarget); err != nil {
		t.Fatalf("reaching the target: %v", err)
	}
	if err := g.deploys.CompleteTarget(ctx, dep.ID, theTarget, targetseam.ReplacementDrained); err != nil {
		t.Fatalf("completing the target: %v", err)
	}
	if err := g.deploys.Complete(ctx, dep.ID); err != nil {
		t.Fatalf("completing the deploy: %v", err)
	}
	// Open itself closes a window that measures nothing timed out, in the same
	// write: there is nothing for the cap to wait on, so no separate close
	// follows.
	if _, err := g.windows.Open(ctx, healthmonitor.Actor, window.OpenEvent{
		DeployID: dep.ID, ReleaseID: rel.ID, BuildID: bl.ID, ServiceID: g.serviceID,
		MeasuresNothing: true, BoundaryVersion: boundary.Version,
		PolicyVersion: "pv_test", ScoreVersion: "scv_test",
	}); err != nil {
		t.Fatalf("opening the window that measures nothing: %v", err)
	}
	return rel
}

// can recompute is one nobody can argue with.
func closedOn() window.Read {
	return window.Read{Quantities: map[gatepolicy.Quantity]boundary.Counts{
		errorRate: {Units: 200, Count: 2, BaselineUnits: 200, BaselineCount: 2},
	}}
}

// readFor is [closedOn] for every exit but skipped, which takes none. A loop
// closing windows at each of the four exits needs the read to follow the exit.
func readFor(exit window.Exit) window.Closing {
	if exit == window.ExitSkipped {
		return window.Closing{}
	}
	return window.Closing{On: closedOn()}
}
