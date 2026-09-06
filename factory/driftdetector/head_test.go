// head_test.go exercises the second comparison, which reads the factory's own
// decision_log table. It is the one file in this package that opens a second
// pool, the factory's own, through package postgres and package decisionlog's
// writer — deps.txt records the edge as "test driftdetector -> postgres lease
// decisionlog".
package driftdetector_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

var logActor = record.Actor{Kind: record.KindComponent, Key: "test", Basis: record.BasisClaimed}

// newFactoryPool is the factory's own store, on a schema of its own with
// the whole factory schema applied, and the token every write here fences
// with.
func newFactoryPool(t *testing.T) (context.Context, *pgxpool.Pool, lease.Token) {
	t.Helper()
	ctx := t.Context()
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "driftdetector_factory_" + hex.EncodeToString(suffix[:])

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
		t.Fatalf("applying the factory's schema: %v", err)
	}
	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("acquiring the lease: %v", err)
	}
	return ctx, pool, token
}

// TestVerifyChainWithNothingRecordedAdoptsTheNewestRowAsTheHead is the
// detector's first pass: nothing to verify against yet, so the newest row
// becomes the head to record.
func TestVerifyChainWithNothingRecordedAdoptsTheNewestRowAsTheHead(t *testing.T) {
	ctx, factory, token := newFactoryPool(t)
	own := newOwnStore(t, ctx)
	log := decisionlog.NewWriter(factory, token)
	first, err := log.AppendPageEvent(ctx, decisionlog.Entry{
		Actor: logActor, Payload: "{}", FormatVersion: "page_event/1",
	})
	if err != nil {
		t.Fatalf("appending a row: %v", err)
	}

	head, mismatch, why, err := driftdetector.VerifyChain(ctx, own, factory)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if mismatch {
		t.Fatalf("VerifyChain over a fresh log reports a mismatch: %s", why)
	}
	if head.Hash != first.Hash || head.Seq != first.Seq {
		t.Errorf("the head read = %+v, want %s at %d", head, first.Hash, first.Seq)
	}
}

// TestVerifyChainExtendsPastARecordedHead is the ordinary pass: the chain
// holds the recorded head, extended and nothing else, and the newest row is
// what the next recording names.
func TestVerifyChainExtendsPastARecordedHead(t *testing.T) {
	ctx, factory, token := newFactoryPool(t)
	own := newOwnStore(t, ctx)
	log := decisionlog.NewWriter(factory, token)

	first, err := log.AppendPageEvent(ctx, decisionlog.Entry{
		Actor: logActor, Payload: "{}", FormatVersion: "page_event/1",
	})
	if err != nil {
		t.Fatalf("appending the first row: %v", err)
	}
	writer := driftdetector.NewWriter(own)
	if _, err := writer.RecordHead(ctx, first.Hash, first.Seq); err != nil {
		t.Fatalf("RecordHead: %v", err)
	}

	second, err := log.AppendPageEvent(ctx, decisionlog.Entry{
		Actor: logActor, Payload: "{}", FormatVersion: "page_event/1",
	})
	if err != nil {
		t.Fatalf("appending the second row: %v", err)
	}

	head, mismatch, why, err := driftdetector.VerifyChain(ctx, own, factory)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if mismatch {
		t.Fatalf("VerifyChain over an extension reports a mismatch: %s", why)
	}
	if head.Hash != second.Hash || head.Seq != second.Seq {
		t.Errorf("the head read = %+v, want the newest row %s at %d", head, second.Hash, second.Seq)
	}
}

// TestVerifyChainCatchesARowEditedInPlace is the whole reason the recorded
// head exists: a payload rewritten after the fact still verifies inside
// decisionlog's own chain-since-restart reasoning, but not against a head
// recorded before the edit.
func TestVerifyChainCatchesARowEditedInPlace(t *testing.T) {
	ctx, factory, token := newFactoryPool(t)
	own := newOwnStore(t, ctx)
	log := decisionlog.NewWriter(factory, token)

	first, err := log.AppendPageEvent(ctx, decisionlog.Entry{
		Actor: logActor, Payload: "{}", FormatVersion: "page_event/1",
	})
	if err != nil {
		t.Fatalf("appending the first row: %v", err)
	}
	writer := driftdetector.NewWriter(own)
	if _, err := writer.RecordHead(ctx, first.Hash, first.Seq); err != nil {
		t.Fatalf("RecordHead: %v", err)
	}
	if _, err := log.AppendPageEvent(ctx, decisionlog.Entry{
		Actor: logActor, Payload: "{}", FormatVersion: "page_event/1",
	}); err != nil {
		t.Fatalf("appending the second row: %v", err)
	}

	if _, err := factory.Exec(ctx, `update `+decisionlog.Table+` set payload = $1 where seq = $2`,
		`{"edited":true}`, first.Seq); err != nil {
		t.Fatalf("editing the first row's payload directly: %v", err)
	}

	_, mismatch, why, err := driftdetector.VerifyChain(ctx, own, factory)
	if err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
	if !mismatch {
		t.Fatal("VerifyChain over a row edited in place reports no mismatch")
	}
	if why == "" {
		t.Error("the mismatch names no reason")
	}
}

// TestRaiseChainMismatchLeavesAStandingOneAlone is what keeps a stopped
// factory from raising a second chain mismatch every pass while the first
// still stands.
func TestRaiseChainMismatchLeavesAStandingOneAlone(t *testing.T) {
	ctx, own := newTableCtx(t)
	writer := driftdetector.NewWriter(own)

	first, err := writer.RaiseChainMismatch(ctx, "the chain no longer holds the recorded head")
	if err != nil {
		t.Fatalf("RaiseChainMismatch: %v", err)
	}
	if first == "" {
		t.Fatal("no mismatch was raised")
	}
	second, err := writer.RaiseChainMismatch(ctx, "found again on a later pass")
	if err != nil {
		t.Fatalf("RaiseChainMismatch again: %v", err)
	}
	if second != "" {
		t.Errorf("a second chain mismatch was raised: %s, want the standing one left alone", second)
	}

	standing, err := driftdetector.UnclearedChain(ctx, own)
	if err != nil {
		t.Fatalf("UnclearedChain: %v", err)
	}
	if len(standing) != 1 || standing[0].ID != first {
		t.Errorf("UnclearedChain = %+v, want exactly the first one raised", standing)
	}
	if standing[0].ServiceID != "" || standing[0].Target != "" {
		t.Errorf("a chain mismatch names service %q target %q, want neither", standing[0].ServiceID, standing[0].Target)
	}
}

// TestMismatchFoldsInAnUnclearedChainMismatch is the gate's own read: a
// chain mismatch holds every service's production deploys, so [Store.Mismatch]
// must answer true for a service with no target mismatch of its own while one
// stands.
func TestMismatchFoldsInAnUnclearedChainMismatch(t *testing.T) {
	ctx, own := newTableCtx(t)
	writer := driftdetector.NewWriter(own)
	if _, err := writer.RaiseChainMismatch(ctx, "the chain no longer holds the recorded head"); err != nil {
		t.Fatalf("RaiseChainMismatch: %v", err)
	}

	held, why, err := driftdetector.NewStore(own).Mismatch(ctx, "some_service_with_no_target_mismatch")
	if err != nil {
		t.Fatalf("Mismatch: %v", err)
	}
	if !held || why == "" {
		t.Errorf("Mismatch = %t %q, want true and a reason: a chain mismatch holds every service", held, why)
	}
}

// TestAddressAndDeliverRoundTrip is installing the detector's own page: the
// address is set once, and a delivery is a record of one send the detector
// made itself.
func TestAddressAndDeliverRoundTrip(t *testing.T) {
	ctx, own := newTableCtx(t)
	if _, err := driftdetector.Address(ctx, own); err == nil {
		t.Error("Address before SetAddress returned no error")
	}

	writer := driftdetector.NewWriter(own)
	if err := writer.SetAddress(ctx, "ops@example.com"); err != nil {
		t.Fatalf("SetAddress: %v", err)
	}
	address, err := driftdetector.Address(ctx, own)
	if err != nil || address != "ops@example.com" {
		t.Fatalf("Address = %q, %v, want ops@example.com", address, err)
	}

	delivered, err := writer.Deliver(ctx, "the notifier's own last check is stale")
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	all, err := driftdetector.OwnDeliveries(ctx, own)
	if err != nil {
		t.Fatalf("OwnDeliveries: %v", err)
	}
	if len(all) != 1 || all[0].ID != delivered.ID || all[0].Why != delivered.Why {
		t.Errorf("OwnDeliveries = %+v, want the one delivery just made", all)
	}
}

// TestLastCheckIsOnePerTargetAcrossServices is finding 29's own fix: the
// record is one per production target and not one per service and target.
func TestLastCheckIsOnePerTargetAcrossServices(t *testing.T) {
	ctx, own := newTableCtx(t)
	writer := driftdetector.NewWriter(own)

	p := pass()
	p.Target = "shared.example"
	if _, err := writer.Record(ctx, p); err != nil {
		t.Fatalf("Record: %v", err)
	}
	p.ServiceID = record.NewID("svc")
	if _, err := writer.Record(ctx, p); err != nil {
		t.Fatalf("Record with a second service on the same target: %v", err)
	}

	checks, err := driftdetector.LastChecks(ctx, own, "")
	if err != nil {
		t.Fatalf("LastChecks: %v", err)
	}
	found := 0
	for _, c := range checks {
		if c.Target == "shared.example" {
			found++
		}
	}
	if found != 1 {
		t.Errorf("LastChecks over one target written for two services = %d rows, want one", found)
	}
}

// newTableCtx is [newTable] for a test that wants no [*driftdetector.Writer]
// of its own.
func newTableCtx(t *testing.T) (context.Context, *pgxpool.Pool) {
	ctx, pool, _ := newTable(t)
	return ctx, pool
}

// newOwnStore is the detector's own store, a fresh schema of its own — the
// second pool a test that also opens the factory's needs, since [newTable]
// gives only one at a time.
func newOwnStore(t *testing.T, _ context.Context) *pgxpool.Pool {
	_, pool, _ := newTable(t)
	return pool
}

// TestAStaleComponentHoldsWhatItReachesAndIsRaisedOnce is the third
// comparison's own record: a component whose last check is past the interval it
// promised holds what that component reaches — the health monitor's a service's
// production deploys, the deployer's an environment's, one row per service in
// it — and a later pass finding it still stale raises no second row.
func TestAStaleComponentHoldsWhatItReachesAndIsRaisedOnce(t *testing.T) {
	ctx, own := newTableCtx(t)
	writer := driftdetector.NewWriter(own)

	raised, err := writer.RaiseStaleComponent(ctx, driftdetector.StaleComponent{
		Component: "health_monitor", ServiceID: "svc_one",
		Why: "the health monitor's own last check for this service is stale",
	})
	if err != nil || raised == "" {
		t.Fatalf("RaiseStaleComponent = %q, %v", raised, err)
	}
	again, err := writer.RaiseStaleComponent(ctx, driftdetector.StaleComponent{
		Component: "health_monitor", ServiceID: "svc_one",
		Why: "the health monitor's own last check for this service is stale",
	})
	if err != nil || again != "" {
		t.Errorf("a second pass over the same stale component = %q, %v, want no second row", again, err)
	}

	// The deployer's record is per target, so its mismatch names one as well and
	// is a row of its own beside the health monitor's.
	perTarget, err := writer.RaiseStaleComponent(ctx, driftdetector.StaleComponent{
		Component: "deployer", ServiceID: "svc_one", Target: "/srv/one",
		Why: "the deployer's own last check for this target is stale",
	})
	if err != nil || perTarget == "" {
		t.Fatalf("RaiseStaleComponent for the deployer = %q, %v", perTarget, err)
	}

	held, why, err := driftdetector.NewStore(own).Mismatch(ctx, "svc_one")
	if err != nil {
		t.Fatalf("Mismatch: %v", err)
	}
	if !held || why == "" {
		t.Errorf("Mismatch = %t %q, want the stopped component holding this service's production deploys", held, why)
	}
	other, _, err := driftdetector.NewStore(own).Mismatch(ctx, "svc_two")
	if err != nil {
		t.Fatalf("Mismatch of another service: %v", err)
	}
	if other {
		t.Error("a stale component's mismatch held a service it does not reach")
	}
	if _, err := writer.RaiseStaleComponent(ctx, driftdetector.StaleComponent{Component: "notifier"}); err == nil {
		t.Error("a stale component naming no service was admitted, and a mismatch that holds nothing stops nothing")
	}
}

// TestStaleAgainstIsTheSafeguardsOwnReading is what a safeguard on the drift
// detector's last check binds: an age of an owner's own, read beside the
// interval the detector supplies for itself.
func TestStaleAgainstIsTheSafeguardsOwnReading(t *testing.T) {
	ctx, own := newTableCtx(t)
	writer := driftdetector.NewWriter(own)
	if _, err := writer.Record(ctx, driftdetector.Pass{
		ServiceID: "svc_one", Target: "/srv/one", Reached: true,
		RunningBuild: "b_one", RecordedBuildID: "b_one", Interval: time.Hour,
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	older, err := driftdetector.StaleAgainst(ctx, own, time.Minute, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("StaleAgainst: %v", err)
	}
	if len(older) != 1 {
		t.Errorf("StaleAgainst read %d check(s) older than a minute an hour later, want the one just written", len(older))
	}
	none, err := driftdetector.StaleAgainst(ctx, own, 0, time.Now())
	if err != nil || len(none) != 0 {
		t.Errorf("StaleAgainst with no age authored = %v, %v, want nothing: no safeguard set one", none, err)
	}
}
