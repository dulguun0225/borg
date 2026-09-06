// The database tests of this package are in deploy_test and open the pool
// through package postgres, the way decisionlog's do; deps.txt records the
// test edge. They apply this package's DDL themselves rather than calling
// postgres.Apply, which does not know this package until integration wires it
// in, and release's beside it, which [deploy.Current] orders by. The target the
// rollout tests reach is [targetseam.NewFake]; localtarget is where a real
// process runs, in that package's own tests.
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
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// newTable gives a test a schema of its own, this package's DDL applied
// inside it, and a writer over it. The schema is dropped when the test ends,
// so a rerun on a database a previous run left dirty starts clean.
func newTable(t *testing.T) (context.Context, *pgxpool.Pool, *deploy.Writer) {
	ctx, pool, w, _ := newTableWithToken(t)
	return ctx, pool, w
}

// newTableWithToken is [newTable] with the lease token as well, for the tests
// that write through another package's writer: one lease, one token, and a
// second acquisition would fence the first writer out.
func newTableWithToken(t *testing.T) (context.Context, *pgxpool.Pool, *deploy.Writer, lease.Token) {
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
	for name, statements := range map[string][]string{
		"lease": lease.DDL, "release": release.DDL, "deploy": deploy.DDL,
	} {
		for n, statement := range statements {
			if _, err := pool.Exec(ctx, statement); err != nil {
				t.Fatalf("applying %s statement %d: %v", name, n+1, err)
			}
		}
	}
	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("acquiring the lease: %v", err)
	}
	return ctx, pool, deploy.NewWriter(pool, token), token
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

// The actor on a deploy record is the deployer and never an agent: deploying is
// not a stage an agent is dispatched to. The principal at the seam is the same
// component, calling as itself.
var (
	deployer      = record.Actor{Kind: record.KindComponent, Key: "deployer"}
	deployerCalls = principal.OfComponent("deployer")
	credential    = secretref.MustNew("deploy.production")
)

// productionID stands for production's environment record. The deploy record
// names an environment by the record's id, and there are no foreign keys between
// record tables, so these tests name one they never create.
const productionID = "env_000000000000000000000000000000a"

// twoTargets is an environment with two targets, in the order a rollout reaches
// them.
var twoTargets = []deploy.Reaching{
	{Address: "/srv/one", ReleaseInstances: 4, ControlInstances: 1, KeptInstances: 2},
	{Address: "/srv/two", ReleaseInstances: 4, KeptInstances: 2},
}

func addressesOf(targets []deploy.Reaching) []string {
	var addresses []string
	for _, target := range targets {
		addresses = append(addresses, target.Address)
	}
	return addresses
}

// mintRelease writes a release so that [deploy.Current] has a number to order
// by. The deploy record names a release by id and the number is the release's,
// which is what orders the current one.
func mintRelease(t *testing.T, ctx context.Context, pool *pgxpool.Pool, token lease.Token, serviceID string) release.Release {
	t.Helper()
	r, err := release.NewWriter(pool, token).Mint(ctx, deployer, release.Minting{
		ServiceID: serviceID,
		BuildID:   record.NewID("bl"),
		Commit:    record.NewID("cm"),
		ItemID:    record.NewID("it"),
	})
	if err != nil {
		t.Fatalf("minting a release: %v", err)
	}
	return r
}

// completeOn marks every named target of the deploy complete, which is what the
// rollout does one target at a time.
func completeOn(t *testing.T, ctx context.Context, w *deploy.Writer, id string, addresses ...string) {
	t.Helper()
	for _, address := range addresses {
		if err := w.ReachTarget(ctx, id, address); err != nil {
			t.Fatalf("reaching %s: %v", address, err)
		}
		if err := w.CompleteTarget(ctx, id, address, targetseam.ReplacementDrained); err != nil {
			t.Fatalf("completing %s: %v", address, err)
		}
	}
}

// TestTheIdentityIsThePairAndASequenceNumber: service and environment are the
// record's grain and not its identity, so two deploys of one release onto one
// environment are two records, numbered per pair as the deployer begins each.
func TestTheIdentityIsThePairAndASequenceNumber(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID, other = "svc_a", "svc_b"
	r := mintRelease(t, ctx, pool, token, serviceID)

	beginning := deploy.Beginning{
		ServiceID: serviceID, EnvironmentID: productionID,
		What: deploy.OfRelease(r.ID, r.BuildID), Targets: twoTargets,
		IntoProduction: true, StrategyPicked: deploy.StrategyWithoutControl,
	}
	first, err := w.Start(ctx, deployer, beginning)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	second, err := w.Start(ctx, deployer, beginning)
	if err != nil {
		t.Fatalf("a second deploy of one release onto one environment: %v", err)
	}
	if first.Number != 1 || second.Number != 2 {
		t.Errorf("the sequence numbers are %d and %d, want 1 and 2", first.Number, second.Number)
	}
	if first.ID == second.ID {
		t.Error("two deploys of one release onto one environment are one record")
	}

	beginning.ServiceID = other
	elsewhere, err := w.Start(ctx, deployer, beginning)
	if err != nil {
		t.Fatalf("Start on another service: %v", err)
	}
	if elsewhere.Number != 1 {
		t.Errorf("another pair's first number is %d, want 1 — the sequence is per pair", elsewhere.Number)
	}

	read, err := deploy.Get(ctx, pool, first.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Number != first.Number || read.ReleaseID != r.ID || read.EnvironmentID != productionID {
		t.Errorf("Get = %+v, want the record Start returned", read)
	}
	if _, err := deploy.Get(ctx, pool, "dep_00000000000000000000000000000000"); !errors.Is(err, deploy.ErrNotFound) {
		t.Errorf("Get of nothing = %v, want %v", err, deploy.ErrNotFound)
	}
}

// TestCompletionIsPerTarget: one record names one release for the whole
// environment and completion is a field per target on it, so a deploy that
// reached one target of two is a recorded partial deploy and not a mismatch
// found after the fact.
func TestCompletionIsPerTarget(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)

	d, err := w.Start(ctx, deployer, deploy.Beginning{
		ServiceID: serviceID, EnvironmentID: productionID,
		What: deploy.OfRelease(r.ID, r.BuildID), Targets: twoTargets,
		IntoProduction: true, StrategyPicked: deploy.StrategyWithControl,
		ControlTarget: "/srv/one", ControlReleaseID: "rel_below",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	targets, err := deploy.Targets(ctx, pool, d.ID)
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	if len(targets) != 2 {
		t.Fatalf("the deploy has %d targets, want the environment's two", len(targets))
	}
	for n, target := range targets {
		if target.Completion != deploy.CompletionNotReached {
			t.Errorf("target %s starts %s, want not reached", target.Address, target.Completion)
		}
		if target.Position != n || target.Address != twoTargets[n].Address {
			t.Errorf("target %d is %s at position %d, want the environment's order", n, target.Address, target.Position)
		}
		if target.Fleets.Kept.Instances != 2 || target.Fleets.Release.Instances != 4 {
			t.Errorf("target %s runs %+v, want the three counts written at the start", target.Address, target.Fleets)
		}
	}

	completeOn(t, ctx, w, d.ID, "/srv/one")
	if err := w.Complete(ctx, d.ID); !errors.Is(err, deploy.ErrTargetsIncomplete) {
		t.Errorf("Complete with one target of two = %v, want ErrTargetsIncomplete", err)
	}

	partial, err := deploy.Targets(ctx, pool, d.ID)
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	if partial[0].Completion != deploy.CompletionComplete || partial[1].Completion != deploy.CompletionNotReached {
		t.Errorf("the targets read %s and %s, want the first complete and the second not reached",
			partial[0].Completion, partial[1].Completion)
	}
	if partial[0].ReachedAt == "" || partial[0].CompleteAt == "" {
		t.Error("the completed target names neither when it was reached nor when it completed")
	}
	if partial[0].Replacement != targetseam.ReplacementDrained {
		t.Errorf("the completed target reports %q, want the drain the seam reported", partial[0].Replacement)
	}

	completeOn(t, ctx, w, d.ID, "/srv/two")
	if err := w.Complete(ctx, d.ID); err != nil {
		t.Fatalf("Complete with every target complete: %v", err)
	}
}

// TestTheStoreRefusesWhatTheWriterDoes inserts around the writer, which is how
// the store's own refusals are tested: the writer's checks are not what this
// asserts.
func TestTheStoreRefusesWhatTheWriterDoes(t *testing.T) {
	ctx, pool, _ := newTable(t)

	const insert = `insert into deploy
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, service_id, environment_id, number,
		 release_id, build_id, status, failed_step, strategy_picked, strategy_performed,
		 snapshot_name, snapshot_digest, snapshot_deleted_at)
		values ($1, $2, 'component', 'deployer', '', $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`

	cases := map[string]struct {
		releaseID, buildID, status, step, picked, performed string
		snapshotName, snapshotDigest, deletedAt             string
		want                                                string
	}{
		"a release with no build": {
			releaseID: "rel_a", status: "started", want: "names_a_build_for_its_release",
		},
		"failed with no step": {
			buildID: "bl_a", status: "failed", want: "failed_names_its_step",
		},
		"a step with no failure": {
			buildID: "bl_a", status: "started", step: "somewhere", want: "failed_names_its_step",
		},
		"a strategy performed under none picked": {
			buildID: "bl_a", status: "started", performed: "with_control",
			want: "performed_names_its_picked",
		},
		"a snapshot with no digest": {
			buildID: "bl_a", status: "started", snapshotName: "before-the-drop", want: "snapshot_names_its_digest",
		},
		"a deletion with no snapshot": {
			buildID: "bl_a", status: "started", deletedAt: record.Now(), want: "snapshot_deleted_names_one",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := pool.Exec(ctx, insert,
				record.NewID(deploy.IDPrefix), deploy.FormatVersion, record.Now(), "svc_a", productionID, 1,
				c.releaseID, c.buildID, c.status, c.step, c.picked, c.performed,
				c.snapshotName, c.snapshotDigest, c.deletedAt)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("the store took the row: %v, want a violation of %s", err, c.want)
			}
		})
	}
}
