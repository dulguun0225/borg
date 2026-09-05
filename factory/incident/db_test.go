// The database tests of this package are in incident_test rather than in
// incident, because they open the pool through package postgres, which
// imports this one to apply its DDL. deps.txt records the edge as "test
// incident -> postgres".
//
// None of these tests skips when the database is unreachable. The milestone
// is demonstrated by them running, so an unreachable database fails the run.
package incident_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

// healthMonitor is the one writer of incidents, the way doc.go names it. A human
// is never one; TestAHumanActorIsRefused is the mirror of that.
var healthMonitor = record.Actor{Kind: record.KindComponent, Key: "health_monitor"}

func newTable(t *testing.T) (context.Context, *pgxpool.Pool, *incident.Writer) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m3_inc_" + hex.EncodeToString(suffix[:])

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
	return ctx, pool, incident.NewWriter(pool, token)
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

// raising is a complete Raising over ids of its own, so a test that needs one
// or several does not repeat all five required fields.
func raising() incident.Raising {
	return incident.Raising{
		EnvironmentID: record.NewID("env"),
		ServiceID:     record.NewID("svc"),
		ReleaseID:     record.NewID("rel"),
		DeployID:      record.NewID("dep"),
		Crossing:      "boundary crossed at 0.4",
	}
}

func TestRaiseWritesTheIncidentOpenWithNoObservations(t *testing.T) {
	ctx, pool, w := newTable(t)
	r := raising()

	raised, err := w.Raise(ctx, healthMonitor, r)
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	if raised.EnvironmentID != r.EnvironmentID || raised.ServiceID != r.ServiceID ||
		raised.ReleaseID != r.ReleaseID || raised.DeployID != r.DeployID || raised.Crossing != r.Crossing {
		t.Errorf("Raise = %+v, which does not name what it was raised over", raised)
	}
	if !raised.Open() {
		t.Error("a freshly raised incident reads as resolved")
	}
	if raised.Observations != 0 {
		t.Errorf("a freshly raised incident has %d observations, want 0", raised.Observations)
	}
	if _, err := time.Parse(record.TimeLayout, raised.At); err != nil {
		t.Errorf("the incident has timestamp %q: %v", raised.At, err)
	}

	read, err := incident.Get(ctx, pool, raised.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read != raised {
		t.Errorf("Get = %+v, want %+v", read, raised)
	}
}

func TestAHumanActorIsRefused(t *testing.T) {
	ctx, pool, w := newTable(t)
	human := record.Actor{Kind: record.KindHuman, Key: "owner", Basis: record.BasisClaimed}

	if _, err := w.Raise(ctx, human, raising()); !errors.Is(err, incident.ErrNotAComponent) {
		t.Errorf("Raise by a human = %v, want ErrNotAComponent", err)
	}

	// Around the writer, the CHECK constraint refuses the same thing.
	r := raising()
	_, err := pool.Exec(ctx, `insert into `+incident.Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, environment_id, service_id, release_id, deploy_id,
		 crossing, intent_id, observations, status, resolved_at)
		values ($1, $2, 'human', 'owner', 'claimed', $3, $4, $5, $6, $7, $8, '', 0, 'open', '')`,
		record.NewID(incident.IDPrefix), incident.FormatVersion, record.Now(), r.EnvironmentID, r.ServiceID, r.ReleaseID, r.DeployID, r.Crossing)
	if err == nil {
		t.Error("the store accepted an incident written by a human")
	}
}

// TestOpenFindsItAndASecondRaiseOnOneServiceAndReleaseIsRefused is the
// deduplication rule doc.go states: an open incident on a service and a
// release makes a further crossing an observation and never a second intent,
// and the partial unique index is the same rule in the store.
func TestOpenFindsItAndASecondRaiseOnOneServiceAndReleaseIsRefused(t *testing.T) {
	ctx, pool, w := newTable(t)
	r := raising()

	if _, found, err := incident.Open(ctx, pool, r.ServiceID, r.ReleaseID); err != nil || found {
		t.Fatalf("Open before anything was raised = found %v, %v", found, err)
	}

	raised, err := w.Raise(ctx, healthMonitor, r)
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	found, ok, err := incident.Open(ctx, pool, r.ServiceID, r.ReleaseID)
	if err != nil || !ok || found.ID != raised.ID {
		t.Fatalf("Open = %+v, found %v, %v", found, ok, err)
	}

	again := raising()
	again.ServiceID, again.ReleaseID = r.ServiceID, r.ReleaseID
	if _, err := w.Raise(ctx, healthMonitor, again); err == nil {
		t.Error("a second open incident on one service and release was accepted")
	}

	if _, err := w.Resolve(ctx, raised.ID); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, found, err := incident.Open(ctx, pool, r.ServiceID, r.ReleaseID); err != nil || found {
		t.Errorf("Open after Resolve = found %v, %v", found, err)
	}
	if _, err := w.Raise(ctx, healthMonitor, again); err != nil {
		t.Errorf("Raise for the same pair after the earlier one resolved = %v, want it accepted", err)
	}
}

func TestObserveRaisesTheCount(t *testing.T) {
	ctx, _, w := newTable(t)
	raised, err := w.Raise(ctx, healthMonitor, raising())
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}

	observed, err := w.Observe(ctx, raised.ID)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if observed.Observations != 1 {
		t.Errorf("Observe = %d observations, want 1", observed.Observations)
	}
	observed, err = w.Observe(ctx, raised.ID)
	if err != nil {
		t.Fatalf("Observe again: %v", err)
	}
	if observed.Observations != 2 {
		t.Errorf("Observe again = %d observations, want 2", observed.Observations)
	}
}

func TestObserveAndResolveOnAResolvedIncidentAreNotOpen(t *testing.T) {
	ctx, _, w := newTable(t)
	raised, err := w.Raise(ctx, healthMonitor, raising())
	if err != nil {
		t.Fatalf("Raise: %v", err)
	}
	if _, err := w.Resolve(ctx, raised.ID); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if _, err := w.Observe(ctx, raised.ID); !errors.Is(err, incident.ErrNotOpen) {
		t.Errorf("Observe on a resolved incident = %v, want ErrNotOpen", err)
	}
	if _, err := w.Resolve(ctx, raised.ID); !errors.Is(err, incident.ErrNotOpen) {
		t.Errorf("Resolve again = %v, want ErrNotOpen", err)
	}
}

func TestObserveAndResolveOnAnUnknownIDAreNotFound(t *testing.T) {
	ctx, _, w := newTable(t)
	const missing = "inc_00000000000000000000000000000000"

	if _, err := w.Observe(ctx, missing); !errors.Is(err, incident.ErrNotFound) {
		t.Errorf("Observe on an unknown id = %v, want ErrNotFound", err)
	}
	if _, err := w.Resolve(ctx, missing); !errors.Is(err, incident.ErrNotFound) {
		t.Errorf("Resolve on an unknown id = %v, want ErrNotFound", err)
	}
}

// TestDDLListsEveryStatus keeps the CHECK constraint and incident.Statuses
// from disagreeing, the way deploy/schema_test.go's
// TestDDLListsEveryStrategyAndStatus does for strategies and statuses: every
// value in incident.Statuses inserts cleanly around the writer, and a value
// outside it does not.
func TestDDLListsEveryStatus(t *testing.T) {
	ctx, pool, _ := newTable(t)

	for _, status := range incident.Statuses {
		r := raising()
		resolvedAt := ""
		if status == incident.StatusResolved {
			resolvedAt = record.Now()
		}
		_, err := pool.Exec(ctx, `insert into `+incident.Table+`
			(id, format_version, actor_kind, actor_key, actor_key_basis, at, environment_id, service_id, release_id, deploy_id,
			 crossing, intent_id, observations, status, resolved_at)
			values ($1, $2, 'component', 'health_monitor', '', $3, $4, $5, $6, $7, $8, '', 0, $9, $10)`,
			record.NewID(incident.IDPrefix), incident.FormatVersion, record.Now(), r.EnvironmentID, r.ServiceID, r.ReleaseID, r.DeployID,
			r.Crossing, string(status), resolvedAt)
		if err != nil {
			t.Errorf("inserting status %q, one of incident.Statuses, was refused: %v", status, err)
		}
	}

	r := raising()
	_, err := pool.Exec(ctx, `insert into `+incident.Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, environment_id, service_id, release_id, deploy_id,
		 crossing, intent_id, observations, status, resolved_at)
		values ($1, $2, 'component', 'health_monitor', '', $3, $4, $5, $6, $7, $8, '', 0, 'flaky', '')`,
		record.NewID(incident.IDPrefix), incident.FormatVersion, record.Now(), r.EnvironmentID, r.ServiceID, r.ReleaseID, r.DeployID, r.Crossing)
	if err == nil {
		t.Error("the store accepted a status outside incident.Statuses")
	}
}

func TestForServiceIsInOrder(t *testing.T) {
	ctx, pool, w := newTable(t)
	serviceID := record.NewID("svc")

	var raised []incident.Incident
	for i := 0; i < 3; i++ {
		r := raising()
		r.ServiceID = serviceID
		got, err := w.Raise(ctx, healthMonitor, r)
		if err != nil {
			t.Fatalf("Raise: %v", err)
		}
		raised = append(raised, got)
	}

	got, err := incident.ForService(ctx, pool, serviceID)
	if err != nil {
		t.Fatalf("ForService: %v", err)
	}
	if len(got) != len(raised) {
		t.Fatalf("ForService returned %d incidents, want %d", len(got), len(raised))
	}
	for n, want := range raised {
		if got[n].ID != want.ID {
			t.Errorf("ForService[%d] = %s, want %s in the order raised", n, got[n].ID, want.ID)
		}
	}
}

func TestARaisingMissingAFieldIsIncomplete(t *testing.T) {
	ctx, _, w := newTable(t)

	for _, c := range []struct {
		what string
		mut  func(*incident.Raising)
	}{
		{"environment", func(r *incident.Raising) { r.EnvironmentID = "" }},
		{"service", func(r *incident.Raising) { r.ServiceID = "" }},
		{"release", func(r *incident.Raising) { r.ReleaseID = "" }},
		{"deploy", func(r *incident.Raising) { r.DeployID = "" }},
		{"crossing", func(r *incident.Raising) { r.Crossing = "" }},
	} {
		r := raising()
		c.mut(&r)
		if _, err := w.Raise(ctx, healthMonitor, r); !errors.Is(err, incident.ErrIncomplete) {
			t.Errorf("Raise missing %s = %v, want ErrIncomplete", c.what, err)
		}
	}
}
