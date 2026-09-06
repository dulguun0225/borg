// exits_test.go is what an exit does to something other than the window it
// closes: the search's window, whose exit is the answer and which rolls nothing
// back; the fleet kept for a rollback, ended at the close of the last window
// that could return to it; and the failure records an incident carries when the
// crossing is found after the window has closed. watch_test.go is the pass
// itself and db_test.go holds the fixtures; the three files are one external
// test package split by subject.
//
// These tests do not skip when the database is unreachable, for the reason
// db_test.go gives.
package healthmonitor_test

import (
	"context"
	"testing"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/window"
)

// TestASearchsWindowEndsWithItsExitAndRollsNothingBack is what Overlapping
// windows states for a search: each of its deploys is measured by a window of
// its own and ends with that window, whatever the exit. The exit is the answer,
// traffic returns to the instances of the rollback's target, which the search
// never tears down — so a crossing here rolls nothing back, raises no incident
// against a release this deploy does not name, and pages nobody.
func TestASearchsWindowEndsWithItsExitAndRollsNothingBack(t *testing.T) {
	ctx, g := newGraph(t)
	shipOne(t, ctx, g, "in_below", window.ExitTimedOut)
	under := shipOne(t, ctx, g, "in_under", window.ExitFailed)

	searching := openSearchWindow(t, ctx, g)

	deployer := &fakeDeployer{}
	pager := &fakePager{}
	monitor := g.monitorWith(t, crossingEmission{rate: 0.5, baselineRate: 0.01, intervals: 8}, deployer, pager)

	watched, err := monitor.Watch(ctx, g.watching())
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if len(watched) != 1 || watched[0].Window.ID != searching.ID {
		t.Fatalf("Watch read %+v, want the search's own window", watched)
	}
	one := watched[0]
	if one.Exit != window.ExitFailed {
		t.Fatalf("the search's window closed %q, want failed: its build crossed", one.Exit)
	}
	if one.WhyNoRollback == "" {
		t.Error("a search's failed exit reports nothing about why it rolled nothing back")
	}
	if len(deployer.calls) != 0 {
		t.Errorf("the deployer was asked for %v at a search's exit, want nothing rolled back and nothing torn down", deployer.calls)
	}
	if len(pager.waits) != 0 {
		t.Errorf("a search's exit fired %+v, want no page: a search meets no page condition", pager.waits)
	}
	if one.IncidentID != "" {
		t.Errorf("the search's crossing raised incident %s against a release its deploy does not name", one.IncidentID)
	}
	raised, err := incident.ForService(ctx, g.pool, g.serviceID)
	if err != nil {
		t.Fatalf("reading the incidents: %v", err)
	}
	if len(raised) != 0 {
		t.Errorf("the search's crossing left %d incident(s), want none", len(raised))
	}
	if _, err := window.Get(ctx, g.pool, searching.ID); err != nil {
		t.Fatalf("reading the search's window back: %v", err)
	}
	// The release under watch was already failed, and the search neither
	// returns to it nor undoes anything further.
	if deployer.rolledTo == under.ID {
		t.Error("the search rolled production back")
	}
}

// TestTheKeptFleetEndsWhenTheLastWindowThatCouldReturnToItCloses is the
// ordering The health monitor states: the instances of the rollback's target
// are kept at full capacity while any open window could return to that release,
// torn down when the last such window closes, and never at an exit of their
// own.
func TestTheKeptFleetEndsWhenTheLastWindowThatCouldReturnToItCloses(t *testing.T) {
	ctx, g := newGraph(t)
	shipOne(t, ctx, g, "in_below", window.ExitTimedOut)
	shipOne(t, ctx, g, "in_first", "")
	second := shipOne(t, ctx, g, "in_second", "")

	deployer := keptRecorded{fakeDeployer: &fakeDeployer{}, g: g}
	// Both arms behaving identically over enough intervals rules the size out,
	// so each window this pass reads closes passed.
	monitor := g.monitorWith(t, crossingEmission{rate: 0.01, baselineRate: 0.01, intervals: 400}, deployer, &fakePager{})

	watched, err := monitor.Watch(ctx, g.watching())
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	if len(watched) != 2 {
		t.Fatalf("Watch read %d window(s), want the two open", len(watched))
	}
	for _, one := range watched {
		if one.Exit != window.ExitPassed {
			t.Fatalf("window %s closed %q, want passed", one.Window.ID, one.Exit)
		}
	}

	// The first window's close left the second one open over a release whose
	// rollback would return to the same one, so nothing was torn down until the
	// second closed.
	if len(deployer.kept) != 2 {
		t.Fatalf("%d kept fleet(s) were torn down, want the two the closed windows keep", len(deployer.kept))
	}
	for _, k := range deployer.kept {
		if k.Target != theTarget || k.OfReleaseID == "" {
			t.Errorf("the kept fleet torn down is %+v, want one naming a target and the release it runs", k)
		}
	}
	dep, found, err := deploy.Current(ctx, g.pool, g.serviceID, theEnvironment, []string{theTarget})
	if err != nil || !found {
		t.Fatalf("reading what production runs: found %t, %v", found, err)
	}
	if dep.ReleaseID != second.ID {
		t.Errorf("production runs %s, want the newest release %s", dep.ReleaseID, second.ID)
	}
}

// TestAnIncidentRaisedAfterTheWindowClosedCarriesTheFailureRecords is what
// Incidents states of every crossing: at the same crossing the health monitor
// copies the failure records for that service, release and target onto the
// incident, a field of it rather than a link to the store — which is the
// material the item raised from it is worked from.
func TestAnIncidentRaisedAfterTheWindowClosedCarriesTheFailureRecords(t *testing.T) {
	ctx, g := newGraph(t)
	g.authorTargets(t, ctx)
	rel := shipOne(t, ctx, g, "in_shipped", window.ExitTimedOut)

	crossing := crossingEmission{rate: 0.01, baselineRate: 0.01, intervals: 8, history: historyCrossing()}
	monitor := g.monitorWith(t, crossing, &fakeDeployer{}, &fakePager{})

	after, found, err := monitor.AfterWindow(ctx, g.watching())
	if err != nil {
		t.Fatalf("AfterWindow: %v", err)
	}
	if !found || !after.Crossed {
		t.Fatalf("AfterWindow = %+v, found %t, want a crossing against the service's own recent history", after, found)
	}
	raised, open, err := incident.Open(ctx, g.pool, g.serviceID, rel.ID)
	if err != nil || !open {
		t.Fatalf("reading the incident: open %t, %v", open, err)
	}
	if raised.FailureRecords == "" {
		t.Error("the incident carries no copy of the failure records at the crossing")
	}
	if raised.Reading != incident.ReadingOwnHistory {
		t.Errorf("the incident names the %q reading, want the one against the service's own recent history", raised.Reading)
	}
}

// keptRecorded is the deployer as the design has it: the health monitor asks
// for the fleet to end, and the deployer closes that fleet's span on the deploy
// record. That record is what a later pass reads, so a fleet already torn down
// is never asked for twice.
type keptRecorded struct {
	*fakeDeployer
	g graph
}

func (d keptRecorded) TearDownKept(ctx context.Context, k healthmonitor.Kept) error {
	if err := d.fakeDeployer.TearDownKept(ctx, k); err != nil {
		return err
	}
	// The hours the span ran are the deployer's arithmetic and are not what
	// this test is about, so the span is closed at none.
	return d.g.deploys.TearDownKept(ctx, k.DeployID, k.Target, 0, deploy.Priced{})
}

// openSearchWindow writes what one step of the search leaves behind: a deploy
// of a build that is on no branch, naming no release, and a window of its own
// over it. The search itself is composed with no builder here, so the records
// are written directly — which is what lets the exit be read on its own.
func openSearchWindow(t *testing.T, ctx context.Context, g graph) window.Window {
	t.Helper()
	dep, err := g.deploys.Start(ctx, theActor, deploy.Beginning{
		ServiceID: g.serviceID, EnvironmentID: theEnvironment,
		What: deploy.OfBuild("bld_search"), Targets: []deploy.Reaching{{Address: theTarget}},
	})
	if err != nil {
		t.Fatalf("starting the search's deploy: %v", err)
	}
	opened, err := g.windows.Open(ctx, healthmonitor.Actor, window.OpenEvent{
		DeployID: dep.ID, BuildID: "bld_search", ServiceID: g.serviceID,
		Size:       map[gatepolicy.Quantity]float64{errorRate: 0.1},
		Power:      map[gatepolicy.Quantity]float64{errorRate: 0.8},
		Confidence: 0.95, CapSeconds: 60,
		BoundaryVersion: boundary.Version, Targets: []string{theTarget},
		OwnHistorySize:         map[gatepolicy.Quantity]float64{errorRate: 0.1},
		OwnHistoryRunLength:    500,
		EmissionVersionRelease: "emission/1", PolicyVersion: "pv_test", ScoreVersion: "scv_test",
	})
	if err != nil {
		t.Fatalf("opening the search's window: %v", err)
	}
	return opened
}
