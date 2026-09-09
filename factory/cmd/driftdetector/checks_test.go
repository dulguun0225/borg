// The third comparison: the factory's own last check records, and what each
// stopped component's mismatch holds. fixtures_test.go holds the two stores, the
// environment and the service.
//
// This file does not skip when its database is unreachable, for the reason
// pass_test.go does not.
package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/lastcheck"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// TestAStoppedRaiseHoldsNothingAndThePageIsTheWholeOfIt is the third
// comparison's own bound. The pass over the advisory feed reaches no deploy, so
// the mismatch its stopping makes holds nothing: it is in no service's uncleared
// answer, and what happens is the page. Holding every service on it would stop
// production deploys across the install because a raise stopped.
func TestAStoppedRaiseHoldsNothingAndThePageIsTheWholeOfIt(t *testing.T) {
	ctx, s, token := newStores(t)
	dir := t.TempDir()
	env, svc, _ := setUp(ctx, t, s.factory, token, dir)
	shipRelease(ctx, t, s.factory, token, svc, env, "c1")

	// The health monitor's own check is fresh, so the pass is not reading every
	// factory last check stale at once, which is the detector's own delivery
	// rather than this comparison.
	writeCheck(ctx, t, s.factory, token, lastcheck.LastCheck{
		Component: lastcheck.ComponentHealthMonitor, Subject: svc.ID, Interval: time.Hour,
	})
	writeCheck(ctx, t, s.factory, token, lastcheck.LastCheck{
		Component: lastcheck.ComponentAdvisoryPass, Interval: time.Minute,
	})
	backdate(ctx, t, s.factory, lastcheck.ComponentAdvisoryPass, time.Now().Add(-time.Hour))

	out := &strings.Builder{}
	if err := staleCheck(ctx, s, out); err != nil {
		t.Fatalf("staleCheck: %v", err)
	}

	held, why, err := driftdetector.NewStore(s.own).Mismatch(ctx, svc.ID)
	if err != nil || held {
		t.Errorf("Mismatch = %v %q, %v; a stopped advisory pass reaches no deploy and holds nothing", held, why, err)
	}
	all, err := driftdetector.Uncleared(ctx, s.own, "")
	if err != nil || len(all) != 1 {
		t.Fatalf("Uncleared = %+v, %v, want the one mismatch the stopped pass raised:\n%s", all, err, out)
	}
	if all[0].Component != lastcheck.ComponentAdvisoryPass || all[0].ServiceID != "" {
		t.Errorf("the mismatch is on %q holding %q, want the advisory pass holding nothing",
			all[0].Component, all[0].ServiceID)
	}
	if !strings.Contains(out.String(), "holding nothing") {
		t.Errorf("the report does not say the mismatch holds nothing:\n%s", out)
	}
}

// TestAStoppedHealthMonitorStillHoldsItsOwnService is the other side of the
// split: that component's record is per service and its stopping leaves the
// release under watch unmeasured, so the mismatch holds that service's
// production deploys.
func TestAStoppedHealthMonitorStillHoldsItsOwnService(t *testing.T) {
	ctx, s, token := newStores(t)
	dir := t.TempDir()
	env, svc, _ := setUp(ctx, t, s.factory, token, dir)
	shipRelease(ctx, t, s.factory, token, svc, env, "c1")

	writeCheck(ctx, t, s.factory, token, lastcheck.LastCheck{
		Component: lastcheck.ComponentHealthMonitor, Subject: svc.ID, Interval: time.Minute,
	})
	writeCheck(ctx, t, s.factory, token, lastcheck.LastCheck{
		Component: lastcheck.ComponentAdvisoryPass, Interval: time.Hour,
	})
	backdate(ctx, t, s.factory, lastcheck.ComponentHealthMonitor, time.Now().Add(-time.Hour))

	out := &strings.Builder{}
	if err := staleCheck(ctx, s, out); err != nil {
		t.Fatalf("staleCheck: %v", err)
	}

	held, _, err := driftdetector.NewStore(s.own).Mismatch(ctx, svc.ID)
	if err != nil || !held {
		t.Errorf("Mismatch = %v, %v; a stopped health monitor holds that service's production deploys:\n%s",
			held, err, out)
	}
}

// TestAComponentFreshAgainRecordsALaterAgreementAndLeavesTheMismatchUncleared
// is the third comparison's own version of the rule a later agreeing target
// pass already keeps: a component whose last check answers fresh again does
// not clear the mismatch its earlier staleness raised, and the agreement is
// recorded on the standing row as the evidence the human clearing it reads.
func TestAComponentFreshAgainRecordsALaterAgreementAndLeavesTheMismatchUncleared(t *testing.T) {
	ctx, s, token := newStores(t)
	dir := t.TempDir()
	env, svc, _ := setUp(ctx, t, s.factory, token, dir)
	shipRelease(ctx, t, s.factory, token, svc, env, "c1")

	writeCheck(ctx, t, s.factory, token, lastcheck.LastCheck{
		Component: lastcheck.ComponentHealthMonitor, Subject: svc.ID, Interval: time.Minute,
	})
	writeCheck(ctx, t, s.factory, token, lastcheck.LastCheck{
		Component: lastcheck.ComponentAdvisoryPass, Interval: time.Hour,
	})
	backdate(ctx, t, s.factory, lastcheck.ComponentHealthMonitor, time.Now().Add(-time.Hour))

	if err := staleCheck(ctx, s, &strings.Builder{}); err != nil {
		t.Fatalf("staleCheck (raising): %v", err)
	}
	raised, err := driftdetector.Uncleared(ctx, s.own, svc.ID)
	if err != nil || len(raised) != 1 {
		t.Fatalf("Uncleared after raising = %+v, %v, want the one mismatch the stale health monitor raised", raised, err)
	}

	// The health monitor's pass runs again and its check is fresh: the
	// component's own record is overwritten with a checked_at of now.
	writeCheck(ctx, t, s.factory, token, lastcheck.LastCheck{
		Component: lastcheck.ComponentHealthMonitor, Subject: svc.ID, Interval: time.Minute,
	})

	out := &strings.Builder{}
	if err := staleCheck(ctx, s, out); err != nil {
		t.Fatalf("staleCheck (fresh again): %v", err)
	}

	held, _, err := driftdetector.NewStore(s.own).Mismatch(ctx, svc.ID)
	if err != nil || !held {
		t.Errorf("Mismatch = %v, %v, want true — a mismatch remains even where a later comparison agrees", held, err)
	}
	standing, err := driftdetector.Get(ctx, s.own, raised[0].ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if standing.LaterAgreements != 1 || standing.Cleared() {
		t.Errorf("standing = %+v, want LaterAgreements 1 and still uncleared", standing)
	}
	if !strings.Contains(out.String(), "fresh again") {
		t.Errorf("the report does not say the component is fresh again:\n%s", out)
	}
}

// TestChainCheckRecordsALaterAgreementOnAStandingMismatch is the second
// comparison's own version of the same rule: finding the chain sound this
// pass does not clear a chain mismatch a previous pass raised, and the
// agreement is recorded on the standing row as evidence.
func TestChainCheckRecordsALaterAgreementOnAStandingMismatch(t *testing.T) {
	ctx, s, token := newStores(t)
	writer := driftdetector.NewWriter(s.own)
	raised, err := writer.RaiseChainMismatch(ctx, "a mismatch found on an earlier pass")
	if err != nil || raised == "" {
		t.Fatalf("RaiseChainMismatch: %q, %v", raised, err)
	}

	log := decisionlog.NewWriter(s.factory, token)
	if _, err := log.AppendPageEvent(ctx, decisionlog.Entry{
		Actor: testActor, Payload: "{}", FormatVersion: "page_event/1",
	}); err != nil {
		t.Fatalf("appending a row: %v", err)
	}

	out := &strings.Builder{}
	if err := chainCheck(ctx, s, out); err != nil {
		t.Fatalf("chainCheck: %v", err)
	}

	standing, err := driftdetector.Get(ctx, s.own, raised)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if standing.LaterAgreements != 1 || standing.Cleared() {
		t.Errorf("standing = %+v, want LaterAgreements 1 and still uncleared", standing)
	}
	if !strings.Contains(out.String(), "later agreement") {
		t.Errorf("the report does not mention the later agreement:\n%s", out)
	}
}

// writeCheck writes one factory last check as the component that keeps it.
func writeCheck(ctx context.Context, t *testing.T, pool *pgxpool.Pool, token lease.Token, c lastcheck.LastCheck) {
	t.Helper()
	actor := record.Actor{Kind: record.KindComponent, Key: c.Component, Basis: record.BasisClaimed}
	if _, err := lastcheck.NewWriter(pool, token).Record(ctx, actor, c); err != nil {
		t.Fatalf("writing %s's last check: %v", c.Component, err)
	}
}

// backdate rewrites one component's rows to look as though its pass ran at that
// time, so a test does not have to sleep past a real interval to see the record
// read as stale.
func backdate(ctx context.Context, t *testing.T, pool *pgxpool.Pool, component string, at time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `update `+lastcheck.Table+` set checked_at = $1 where component = $2`,
		record.FormatTime(at), component); err != nil {
		t.Fatalf("backdating %s's last check: %v", component, err)
	}
}
