// The database tests of this package are in lastcheck_test rather than in
// lastcheck, because they open the pool through package postgres, which imports
// this one to apply its DDL. deps.txt records the edge as
// "test lastcheck -> postgres".
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package lastcheck_test

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

	"github.com/dulguun0225/borg/factory/lastcheck"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

var healthMonitor = record.Actor{Kind: record.KindComponent, Key: "health monitor"}

var deployer = record.Actor{Kind: record.KindComponent, Key: "deployer"}

func newTable(t *testing.T) (context.Context, *pgxpool.Pool, *lastcheck.Writer) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "lc_" + hex.EncodeToString(suffix[:])

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
	return ctx, pool, lastcheck.NewWriter(pool, token)
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

// TestARecordIsOverwrittenPerComponentAndSubject: the record is overwritten each
// pass rather than appended to, so a component's newest pass is one row and the
// counts it reports are that pass's.
func TestARecordIsOverwrittenPerComponentAndSubject(t *testing.T) {
	ctx, pool, w := newTable(t)

	first, err := w.Record(ctx, healthMonitor, lastcheck.LastCheck{
		Component: lastcheck.ComponentHealthMonitor,
		Subject:   "svc_one",
		Interval:  5 * time.Minute,
		Payload:   `{"windows":1}`,
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if first.CheckedAt == "" {
		t.Error("the writer stored no time of its own")
	}

	second, err := w.Record(ctx, healthMonitor, lastcheck.LastCheck{
		Component: lastcheck.ComponentHealthMonitor,
		Subject:   "svc_one",
		Interval:  5 * time.Minute,
		Payload:   `{"windows":2}`,
	})
	if err != nil {
		t.Fatalf("Record again: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("the second pass wrote a second row (%s then %s), want one overwritten", first.ID, second.ID)
	}

	// A second subject of the same component is a second row: one process is not
	// one failure, so the health monitor writes one per service.
	if _, err := w.Record(ctx, healthMonitor, lastcheck.LastCheck{
		Component: lastcheck.ComponentHealthMonitor,
		Subject:   "svc_two",
		Interval:  5 * time.Minute,
	}); err != nil {
		t.Fatalf("Record for a second service: %v", err)
	}

	all, err := lastcheck.All(ctx, pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("All = %d rows, want 2", len(all))
	}

	read, found, err := lastcheck.Get(ctx, pool, lastcheck.ComponentHealthMonitor, "svc_one")
	if err != nil || !found {
		t.Fatalf("Get = found %v, %v", found, err)
	}
	if read.Payload != `{"windows":2}` {
		t.Errorf("the row carries %q, want the second pass's counts", read.Payload)
	}
	if read.Interval != 5*time.Minute {
		t.Errorf("the interval reads back as %v, want 5m", read.Interval)
	}
	if !read.FurtherPassOwed() {
		t.Error("a pass that said nothing about being the last is read as owing no further one")
	}

	if _, found, err := lastcheck.Get(ctx, pool, lastcheck.ComponentHealthMonitor, "svc_missing"); err != nil || found {
		t.Errorf("Get of a subject with no record = found %v, %v", found, err)
	}
}

// TestOnlyTheSixComponentsInThisStoreMayWriteOne: the seventh last check is the
// drift detector's, which lives in a store of its own that no factory component
// may write. The writer refuses it and so does the store.
func TestOnlyTheSixComponentsInThisStoreMayWriteOne(t *testing.T) {
	ctx, pool, w := newTable(t)

	_, err := w.Record(ctx, deployer, lastcheck.LastCheck{
		Component: "drift_detector",
		Subject:   "/srv/targets/one",
		Interval:  time.Minute,
	})
	if !errors.Is(err, lastcheck.ErrComponentUnknown) {
		t.Errorf("recording the drift detector's here = %v, want ErrComponentUnknown", err)
	}

	if _, err := pool.Exec(ctx, `insert into `+lastcheck.Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, component, subject, checked_at, interval_seconds, further_pass_owed, payload)
		values ('lc_x', $1, 'component', 'drift detector', '', $2, 'drift_detector', '/srv', $2, 60, true, '')`,
		lastcheck.FormatVersion, record.Now()); err == nil {
		t.Error("the store accepted a component written around the writer")
	}
}

// TestDDLListsEveryComponent keeps the CHECK constraint and [lastcheck.Components]
// from disagreeing: the constraint is SQL text rather than built from the slice,
// so this is what says they still name the same components.
func TestDDLListsEveryComponent(t *testing.T) {
	const open = "constraint component_known check (component in ("
	statement := lastcheck.DDL[0]
	i := strings.Index(statement, open)
	if i < 0 {
		t.Fatalf("the DDL has no %q list", open)
	}
	rest := statement[i+len(open):]
	listed := strings.Split(rest[:strings.Index(rest, ")")], ",")
	if len(listed) != len(lastcheck.Components) {
		t.Fatalf("the constraint lists %d components, Components has %d", len(listed), len(lastcheck.Components))
	}
	for n, c := range lastcheck.Components {
		if got, want := strings.TrimSpace(listed[n]), "'"+c+"'"; got != want {
			t.Errorf("the constraint lists %s where Components has %s", got, want)
		}
	}
}

// TestASubjectIsRequiredOfTheComponentsThatKeepOnePerThing: the health monitor
// keeps one per service and the deployer one per target and one per platform, so
// each names its subject; the notifier, the two passes and dispatch keep a single
// one for themselves and name none.
func TestASubjectIsRequiredOfTheComponentsThatKeepOnePerThing(t *testing.T) {
	ctx, pool, w := newTable(t)

	if _, err := w.Record(ctx, healthMonitor, lastcheck.LastCheck{
		Component: lastcheck.ComponentHealthMonitor,
		Interval:  time.Minute,
	}); !errors.Is(err, lastcheck.ErrSubjectDoesNotMatchComponent) {
		t.Errorf("the health monitor's naming no service = %v, want ErrSubjectDoesNotMatchComponent", err)
	}
	notifier := record.Actor{Kind: record.KindComponent, Key: "notifier"}
	if _, err := w.Record(ctx, notifier, lastcheck.LastCheck{
		Component: lastcheck.ComponentNotifier,
		Subject:   "something",
		Interval:  time.Minute,
	}); !errors.Is(err, lastcheck.ErrSubjectDoesNotMatchComponent) {
		t.Errorf("the notifier's naming a subject = %v, want ErrSubjectDoesNotMatchComponent", err)
	}
	if _, err := w.Record(ctx, notifier, lastcheck.LastCheck{
		Component: lastcheck.ComponentNotifier,
		Interval:  time.Minute,
	}); err != nil {
		t.Fatalf("the notifier's single record for itself: %v", err)
	}

	if _, err := pool.Exec(ctx, `insert into `+lastcheck.Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, component, subject, checked_at, interval_seconds, further_pass_owed, payload)
		values ('lc_y', $1, 'component', 'notifier', '', $2, 'notifier', 'something', $2, 60, true, '')`,
		lastcheck.FormatVersion, record.Now()); err == nil {
		t.Error("the store accepted the notifier's record naming a subject")
	}
}

// TestAnIntervalIsRequiredAndAHumanWritesNone: the age that means stopped has to
// be readable by something that authored nothing, so the interval is on the record
// and is never absent; and a last check is the writing component's own record of
// its own pass, so a human does not write one.
func TestAnIntervalIsRequiredAndAHumanWritesNone(t *testing.T) {
	ctx, pool, w := newTable(t)

	if _, err := w.Record(ctx, healthMonitor, lastcheck.LastCheck{
		Component: lastcheck.ComponentHealthMonitor,
		Subject:   "svc_one",
	}); !errors.Is(err, lastcheck.ErrIntervalNotPositive) {
		t.Errorf("a record naming no interval = %v, want ErrIntervalNotPositive", err)
	}

	owner := record.Actor{Kind: record.KindHuman, Key: "owner", Basis: record.BasisClaimed}
	if _, err := w.Record(ctx, owner, lastcheck.LastCheck{
		Component: lastcheck.ComponentHealthMonitor,
		Subject:   "svc_one",
		Interval:  time.Minute,
	}); !errors.Is(err, lastcheck.ErrNotAComponent) {
		t.Errorf("a human recording a pass = %v, want ErrNotAComponent", err)
	}

	if _, err := pool.Exec(ctx, `insert into `+lastcheck.Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, component, subject, checked_at, interval_seconds, further_pass_owed, payload)
		values ('lc_z', $1, 'human', 'owner', 'claimed', $2, 'health_monitor', 'svc_one', $2, 60, true, '')`,
		lastcheck.FormatVersion, record.Now()); err == nil {
		t.Error("the store accepted a human's last check")
	}
	if _, err := pool.Exec(ctx, `insert into `+lastcheck.Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, component, subject, checked_at, interval_seconds, further_pass_owed, payload)
		values ('lc_w', $1, 'component', 'health monitor', '', $2, 'health_monitor', 'svc_one', $2, 0, true, '')`,
		lastcheck.FormatVersion, record.Now()); err == nil {
		t.Error("the store accepted an interval of nothing")
	}
}

// TestStaleIsPastTheIntervalWithAFurtherPassOwed: a record past the interval it
// names with a further pass owed is always something that stopped and never
// something that went away, which is what the home view and the drift detector's
// third comparison each read.
func TestStaleIsPastTheIntervalWithAFurtherPassOwed(t *testing.T) {
	ctx, pool, w := newTable(t)

	if _, err := w.Record(ctx, healthMonitor, lastcheck.LastCheck{
		Component: lastcheck.ComponentHealthMonitor,
		Subject:   "svc_stopped",
		Interval:  time.Minute,
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	// The retired service's: the component records on its last pass that no
	// further pass is owed, and the row stops being something that stopped.
	if _, err := w.Record(ctx, healthMonitor, lastcheck.LastCheck{
		Component: lastcheck.ComponentHealthMonitor,
		Subject:   "svc_retired",
		Interval:  time.Minute,
		LastPass:  true,
	}); err != nil {
		t.Fatalf("Record of a last pass: %v", err)
	}
	if _, err := w.Record(ctx, deployer, lastcheck.LastCheck{
		Component: lastcheck.ComponentDeployer,
		Subject:   "/srv/targets/one",
		Interval:  time.Hour,
	}); err != nil {
		t.Fatalf("Record for a target: %v", err)
	}

	// Nothing is stale a moment after it was written.
	stale, err := lastcheck.Stale(ctx, pool, time.Now())
	if err != nil {
		t.Fatalf("Stale: %v", err)
	}
	if len(stale) != 0 {
		t.Fatalf("Stale immediately after three passes = %v, want none", stale)
	}

	// Two minutes on, the one-minute interval has missed a pass and the one-hour
	// interval has not; the retired service's is past its interval and owes none.
	stale, err = lastcheck.Stale(ctx, pool, time.Now().Add(2*time.Minute))
	if err != nil {
		t.Fatalf("Stale: %v", err)
	}
	if len(stale) != 1 || stale[0].Subject != "svc_stopped" {
		t.Fatalf("Stale = %+v, want the stopped service's alone", stale)
	}

	forComponent, err := lastcheck.ForComponent(ctx, pool, lastcheck.ComponentHealthMonitor)
	if err != nil {
		t.Fatalf("ForComponent: %v", err)
	}
	if len(forComponent) != 2 {
		t.Errorf("ForComponent = %d rows, want the two the health monitor wrote", len(forComponent))
	}
}

// TestTheDeployersPlatformRecordCarriesTheThreeCounts: how much room a platform
// has is read rather than modelled, so the deployer's pass over a platform
// writes what the records hold, what the platform reports holding, and the room
// the platform reports where it reports one.
func TestTheDeployersPlatformRecordCarriesTheThreeCounts(t *testing.T) {
	ctx, pool, w := newTable(t)

	written, err := w.RecordPlatformPass(ctx, deployer, "acme-cloud", time.Hour, lastcheck.PlatformPass{
		StandingByTheRecords: 3,
		HeldByThePlatform:    5,
		Room:                 8,
		RoomReported:         true,
	})
	if err != nil {
		t.Fatalf("RecordPlatformPass: %v", err)
	}
	if written.Component != lastcheck.ComponentDeployer || written.Subject != "acme-cloud" {
		t.Fatalf("the pass was written as %s over %q", written.Component, written.Subject)
	}

	read, found, err := lastcheck.Get(ctx, pool, lastcheck.ComponentDeployer, "acme-cloud")
	if err != nil || !found {
		t.Fatalf("Get: %v, found %v", err, found)
	}
	pass, err := lastcheck.PlatformPassOf(read)
	if err != nil {
		t.Fatalf("PlatformPassOf: %v", err)
	}
	if pass.StandingByTheRecords != 3 || pass.HeldByThePlatform != 5 || pass.Room != 8 || !pass.RoomReported {
		t.Errorf("the record reports %+v", pass)
	}
	// What the platform holds beyond what the records say stands is a teardown
	// that failed, which the deployer tears down again on its next pass.
	if pass.Leaked() != 2 {
		t.Errorf("the pass reports %d leaked, want 2", pass.Leaked())
	}

	// Where the platform reports no room figure, the two counts are what a
	// reader shows and nothing computes a third.
	if _, err := w.RecordPlatformPass(ctx, deployer, "acme-cloud", time.Hour, lastcheck.PlatformPass{
		StandingByTheRecords: 3,
		HeldByThePlatform:    3,
	}); err != nil {
		t.Fatalf("RecordPlatformPass without a room figure: %v", err)
	}
	read, _, err = lastcheck.Get(ctx, pool, lastcheck.ComponentDeployer, "acme-cloud")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	pass, err = lastcheck.PlatformPassOf(read)
	if err != nil {
		t.Fatalf("PlatformPassOf: %v", err)
	}
	if pass.RoomReported || pass.Room != 0 || pass.Leaked() != 0 {
		t.Errorf("a platform that reported no room reads as %+v", pass)
	}

	// The platform's name is what the record is keyed on, so a pass naming none
	// is refused rather than overwriting the record the deployer keeps for
	// itself — which is a record it does not keep.
	if _, err := w.RecordPlatformPass(ctx, deployer, "", time.Hour, lastcheck.PlatformPass{}); err == nil {
		t.Error("a pass naming no platform was written")
	}
}
