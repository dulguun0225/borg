// pages_test.go is the page condition that is about a rollback rather than
// about a window: one this component called for whose deploy record is still
// not complete on every target at the deployer's next last check for that
// environment. watch_test.go holds the other page this package fires, an open
// incident whose crossing has not stopped with no window open.
package healthmonitor_test

import (
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/lastcheck"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/targetseam"
	"github.com/dulguun0225/borg/factory/window"
)

// TestARollbackNotCompleteAtTheDeployersNextCheckPages is the fifth page
// condition. Production serves a release the factory has already failed and the
// mechanism that would remove it did not finish — the deployer stopped, or a
// target accepted the shift and never completed it. The deployer's own last
// check is what makes it readable and what bounds it: a rollback still running
// is not one that stopped, so nothing fires until the deployer has recorded a
// pass since the rollback's record was written.
func TestARollbackNotCompleteAtTheDeployersNextCheckPages(t *testing.T) {
	ctx, g := newGraph(t)
	g.authorTargets(t, ctx)
	below := shipOne(t, ctx, g, "in_below", window.ExitTimedOut)
	failed := shipOne(t, ctx, g, "in_failed", window.ExitFailed)

	// The rollback the health monitor called for, started and never completed on
	// its target.
	rollback, err := g.deploys.StartUndoing(ctx, theActor, deploy.Beginning{
		ServiceID: g.serviceID, EnvironmentID: g.environmentID,
		What:    deploy.OfRelease(below.ID, below.BuildID),
		Targets: []deploy.Reaching{{Address: theTarget, KeptInstances: 1}},
	}, deploy.Undoing{FailedReleaseID: failed.ID, Source: deploy.SourceHealthMonitorAtFailed})
	if err != nil {
		t.Fatalf("writing the rollback's deploy record: %v", err)
	}

	pager := &fakePager{}
	monitor := g.monitorWith(t, crossingEmission{rate: 0.01, baselineRate: 0.01, intervals: 4}, &fakeDeployer{}, pager)

	// Nothing fires while the deployer has recorded no pass since: a rollback
	// under way is not one that stopped.
	paged, err := monitor.PageRollbackNotComplete(ctx, g.watching())
	if err != nil {
		t.Fatalf("PageRollbackNotComplete before the deployer's next check: %v", err)
	}
	if paged != "" || len(pager.waits) != 0 {
		t.Fatalf("a rollback with no deployer pass since it was called for paged %q through %+v", paged, pager.waits)
	}

	recordDeployerPass(t, g)

	paged, err = monitor.PageRollbackNotComplete(ctx, g.watching())
	if err != nil {
		t.Fatalf("PageRollbackNotComplete: %v", err)
	}
	if paged != rollback.ID || len(pager.waits) != 1 {
		t.Fatalf("the pass paged %q through %d wait(s), want the one rollback that did not finish", paged, len(pager.waits))
	}
	fired := pager.waits[0]
	if fired.Kind != notifier.KindRollbackIncomplete || !fired.Worse || !fired.RollbackOutstanding {
		t.Errorf("the wait is %+v, want a rollback-incomplete of the first kind, which pages at whatever hour it arose", fired)
	}
	if fired.ServiceID != g.serviceID || fired.Row != rollback.ID {
		t.Errorf("the wait is %+v, want one naming the service and the rollback's own record", fired)
	}

	// The condition stands and the page is a sequence on one row: the next pass
	// widens it once to the owner, and every pass after that writes nothing.
	if _, err := monitor.PageRollbackNotComplete(ctx, g.watching()); err != nil {
		t.Fatalf("PageRollbackNotComplete on the second pass: %v", err)
	}
	if len(pager.waits) != 2 {
		t.Fatalf("the second pass left %d wait(s), want the one widening", len(pager.waits))
	}
	for i := 0; i < 3; i++ {
		if _, err := monitor.PageRollbackNotComplete(ctx, g.watching()); err != nil {
			t.Fatalf("PageRollbackNotComplete on a later pass: %v", err)
		}
	}
	if len(pager.waits) != 2 {
		t.Errorf("a standing condition wrote %d page event(s), want the reach and the one widening", len(pager.waits))
	}

	// Completed on every target, the condition is gone and a fresh pager writes
	// nothing.
	if err := g.deploys.ReachTarget(ctx, rollback.ID, theTarget); err != nil {
		t.Fatalf("reaching the target: %v", err)
	}
	if err := g.deploys.CompleteTarget(ctx, rollback.ID, theTarget, targetseam.ReplacementDrained); err != nil {
		t.Fatalf("completing the target: %v", err)
	}
	if err := g.deploys.Complete(ctx, rollback.ID); err != nil {
		t.Fatalf("completing the rollback: %v", err)
	}
	after := &fakePager{}
	paged, err = g.monitorWith(t, crossingEmission{}, &fakeDeployer{}, after).
		PageRollbackNotComplete(ctx, g.watching())
	if err != nil {
		t.Fatalf("PageRollbackNotComplete over a completed rollback: %v", err)
	}
	if paged != "" || len(after.waits) != 0 {
		t.Errorf("a completed rollback paged %q through %+v", paged, after.waits)
	}
}

// theDeployer is who a last check about reaching a target is written as: the
// deployer's own record of its own pass, which is what the condition above is
// read at.
var theDeployer = record.Actor{Kind: record.KindComponent, Key: "deployer", Basis: record.BasisClaimed}

// recordDeployerPass writes the deployer's own last check over the one target
// this service runs on, which is the deployer's next last check for the
// environment: the condition above is read at one.
func recordDeployerPass(t *testing.T, g graph) {
	t.Helper()
	ctx := t.Context()
	writer := lastcheck.NewWriter(g.pool, g.token)
	_, err := writer.Record(ctx, theDeployer, lastcheck.LastCheck{
		Component: lastcheck.ComponentDeployer, Subject: theTarget, Interval: time.Minute,
	})
	if err != nil {
		t.Fatalf("recording the deployer's pass over %s: %v", theTarget, err)
	}
}
