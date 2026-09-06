// fixtures_test.go is the plumbing pass_test.go's tests share: the two
// stores, a production environment and a service, a shipped or a started
// release, and a target that reports what a test wants it to. Splitting it
// out of pass_test.go is what keeps that file under the line bound with the
// tests read together.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/project"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// testActor is who these tests write the factory's records as, a component
// like any other actor here — nothing in these tests is a human's act.
var testActor = record.Actor{Kind: record.KindComponent, Key: "test", Basis: record.BasisClaimed}

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
// that release into env, on every target — so [deploy.Current] names it, the
// way a production deploy the factory performed would.
func shipRelease(ctx context.Context, t *testing.T, pool *pgxpool.Pool, token lease.Token, svc service.Service, env environment.Environment, commitHash string) deploy.Deploy {
	t.Helper()
	d := startRelease(ctx, t, pool, token, svc, env, commitHash)
	w := deploy.NewWriter(pool, token)
	for _, target := range env.Targets {
		if err := w.CompleteTarget(ctx, d.ID, target.Address, targetseam.ReplacementDrained); err != nil {
			t.Fatalf("completing target %s of %s: %v", target.Address, d.ID, err)
		}
	}
	if err := w.Complete(ctx, d.ID); err != nil {
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
		ItemID:                itemID,
		ServiceID:             svc.ID,
		CommitHash:            commitHash,
		ArtifactDigest:        "sha256:" + commitHash,
		ShippedBundleIdentity: "bundle-test",
	})
	if err != nil {
		t.Fatalf("creating the build: %v", err)
	}
	rel, err := release.NewWriter(pool, token).Mint(ctx, testActor, release.Minting{
		ServiceID: svc.ID, BuildID: b.ID, Commit: commitHash, ItemID: itemID,
	})
	if err != nil {
		t.Fatalf("minting the release: %v", err)
	}
	targets := make([]deploy.Reaching, len(env.Targets))
	for n, target := range env.Targets {
		targets[n] = deploy.Reaching{Address: target.Address}
	}
	d, err := deploy.NewWriter(pool, token).Start(ctx, testActor, deploy.Beginning{
		ServiceID: svc.ID, EnvironmentID: env.ID, What: deploy.OfRelease(rel.ID, b.ID),
		Targets: targets, IntoProduction: true, StrategyPicked: deploy.StrategyWithoutControl,
	})
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

func (erroringTarget) Deploy(context.Context, principal.Principal, targetseam.Deployment) (targetseam.Placement, error) {
	return targetseam.Placement{}, nil
}
func (erroringTarget) Stop(context.Context, principal.Principal, string, secretref.Ref) (targetseam.Placement, error) {
	return targetseam.Placement{}, nil
}
func (e erroringTarget) ReadRunning(context.Context, principal.Principal, string, secretref.Ref) (targetseam.Running, error) {
	return targetseam.Running{}, e.err
}
func (erroringTarget) ShiftTraffic(context.Context, principal.Principal, targetseam.Shift) error {
	return nil
}
func (erroringTarget) SetInstanceCount(context.Context, principal.Principal, targetseam.InstanceCount) error {
	return nil
}
func (erroringTarget) ApplySchemaChange(context.Context, principal.Principal, targetseam.SchemaChange) error {
	return nil
}
func (erroringTarget) Snapshot(context.Context, principal.Principal, targetseam.SnapshotRequest) (targetseam.Snapshot, error) {
	return targetseam.Snapshot{}, nil
}
