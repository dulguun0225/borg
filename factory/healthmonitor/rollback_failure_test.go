package healthmonitor_test

import (
	"fmt"
	"testing"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/window"
)

// TestARollbackThatCannotReachItsTargetStillRaisesPagesAndCloses is the
// failed exit's target-unreachable path: the rollback has no record to leave
// for the later standing-rollback page, so this pass raises the incident,
// pages the failed production state, and records the failed exit.
func TestARollbackThatCannotReachItsTargetStillRaisesPagesAndCloses(t *testing.T) {
	ctx, g := newGraph(t)
	shipOne(t, ctx, g, "in_below", window.ExitTimedOut)
	under := shipOne(t, ctx, g, "in_under", "")

	deployer := &fakeDeployer{rollbackErr: fmt.Errorf("%w: target unreachable", deploy.ErrTargetRefused)}
	pager := &fakePager{}
	monitor := g.monitorWith(t, crossingEmission{rate: 0.5, baselineRate: 0.01, intervals: 8}, deployer, pager)

	watched, err := monitor.Watch(ctx, g.watching())
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if len(watched) != 1 || watched[0].Exit != window.ExitFailed {
		t.Fatalf("Watch = %+v, want one failed exit", watched)
	}
	if watched[0].IncidentID == "" || watched[0].WhyNoRollback == "" {
		t.Errorf("failed exit is %+v, want an incident and rollback failure", watched[0])
	}
	if len(pager.waits) != 1 || pager.waits[0].Kind != notifier.KindCredentialUnreachable {
		t.Errorf("pages = %+v, want one credential-unreachable page", pager.waits)
	}
	closed, found, err := window.ForRelease(ctx, g.pool, under.ID)
	if err != nil || !found || closed.Exit != window.ExitFailed {
		t.Errorf("stored window = %+v, found %t, err %v, want failed", closed, found, err)
	}
	if deployer.rolledTo != "" {
		t.Errorf("deployer state = %+v, want no completed rollback", deployer)
	}
}
