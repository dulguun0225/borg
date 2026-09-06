// pass is exercised directly here rather than through passCommand, because it
// is the whole of the comparison and passCommand is only the flag parsing and
// the two stores' plumbing around it. It is an internal test — package main —
// because pass is unexported. fixtures_test.go holds the two stores, a
// production environment and a service, a shipped or a started release, and
// the erroring target these tests share.
//
// Neither this file nor fixtures_test.go skips when its database is
// unreachable: the milestone is demonstrated by them running, so an
// unreachable database fails the run.
package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/project"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/targetseam"
	"github.com/dulguun0225/borg/factory/window"
)

func TestAPassWhereTheTargetAgreesRaisesNoMismatch(t *testing.T) {
	ctx, s, token := newStores(t)
	dir := t.TempDir()
	env, svc, credential := setUp(ctx, t, s.factory, token, dir)
	current := shipRelease(ctx, t, s.factory, token, svc, env, "c1")
	recordRunning(t, dir, testServiceName, current.BuildID)

	out := &strings.Builder{}
	targetAt := func(dir string) targetseam.Target { return localtarget.New(dir) }
	if err := pass(ctx, s, out, credential, targetAt); err != nil {
		t.Fatalf("pass: %v", err)
	}

	if !strings.Contains(out.String(), "agrees") {
		t.Errorf("the report does not say the target agrees:\n%s", out)
	}
	held, why, err := driftdetector.NewStore(s.own).Mismatch(ctx, svc.ID)
	if err != nil || held {
		t.Errorf("Mismatch = %v %q, %v; a target running the recorded build raises none", held, why, err)
	}
	checks, err := driftdetector.LastChecks(ctx, s.own, svc.ID)
	if err != nil || len(checks) != 1 || !checks[0].Agreed {
		t.Errorf("LastChecks = %+v, %v, want one check agreeing", checks, err)
	}
}

// TestAPassWhereTheTargetDisagreesRaisesAMismatchTheReportNames is the row
// the production deploy gate holds on: raised by the pass, and named in the
// report a human at the drift detector reads.
func TestAPassWhereTheTargetDisagreesRaisesAMismatchTheReportNames(t *testing.T) {
	ctx, s, token := newStores(t)
	dir := t.TempDir()
	env, svc, credential := setUp(ctx, t, s.factory, token, dir)
	current := shipRelease(ctx, t, s.factory, token, svc, env, "c1")
	recordRunning(t, dir, testServiceName, "bl_somebodyelses")

	out := &strings.Builder{}
	targetAt := func(dir string) targetseam.Target { return localtarget.New(dir) }
	if err := pass(ctx, s, out, credential, targetAt); err != nil {
		t.Fatalf("pass: %v", err)
	}

	if !strings.Contains(out.String(), "MISMATCH") {
		t.Errorf("the report does not name the mismatch:\n%s", out)
	}
	uncleared, err := driftdetector.Uncleared(ctx, s.own, svc.ID)
	if err != nil || len(uncleared) != 1 {
		t.Fatalf("Uncleared = %+v, %v, want the one mismatch just raised", uncleared, err)
	}
	m := uncleared[0]
	if m.RunningBuild != "bl_somebodyelses" || m.RecordedBuildID != current.BuildID {
		t.Errorf("the mismatch reads running %q recorded %q, want %q and %q",
			m.RunningBuild, m.RecordedBuildID, "bl_somebodyelses", current.BuildID)
	}
}

func TestATargetThatErrorsOnReadRunningWritesAnUnreachedLastCheckAndRaisesNoMismatch(t *testing.T) {
	ctx, s, token := newStores(t)
	dir := t.TempDir()
	env, svc, credential := setUp(ctx, t, s.factory, token, dir)
	shipRelease(ctx, t, s.factory, token, svc, env, "c1")

	failure := errors.New("dial tcp: connection refused")
	targetAt := func(string) targetseam.Target { return erroringTarget{err: failure} }
	out := &strings.Builder{}
	if err := pass(ctx, s, out, credential, targetAt); err != nil {
		t.Fatalf("pass: %v", err)
	}

	if !strings.Contains(out.String(), "could not be reached") {
		t.Errorf("the report does not say the target could not be reached:\n%s", out)
	}
	checks, err := driftdetector.LastChecks(ctx, s.own, svc.ID)
	if err != nil {
		t.Fatalf("LastChecks: %v", err)
	}
	if len(checks) != 1 || checks[0].Reached || checks[0].Why != failure.Error() {
		t.Errorf("LastChecks = %+v, want one unreached check naming %q", checks, failure)
	}
	held, _, err := driftdetector.NewStore(s.own).Mismatch(ctx, svc.ID)
	if err != nil || held {
		t.Errorf("Mismatch = %v, %v; failing to reach a target is not a mismatch", held, err)
	}
}

func TestNoProductionEnvironmentIsNothingToCheckAndWritesNothing(t *testing.T) {
	ctx, s, token := newStores(t)
	credential := secretref.MustNew("deploy.local")
	prj, err := project.NewWriter(s.factory, token).Create(ctx, testActor, "default")
	if err != nil {
		t.Fatalf("creating the project: %v", err)
	}
	if _, err := service.NewWriter(s.factory, token).Create(ctx, testActor, testServiceName, "github.com/example/demo", prj.ID); err != nil {
		t.Fatalf("creating the service: %v", err)
	}
	calls := 0
	targetAt := func(dir string) targetseam.Target { calls++; return localtarget.New(dir) }

	out := &strings.Builder{}
	if err := pass(ctx, s, out, credential, targetAt); err != nil {
		t.Fatalf("pass: %v", err)
	}
	if !strings.Contains(out.String(), "no production environment") {
		t.Errorf("the report does not say there is no production environment:\n%s", out)
	}
	if calls != 0 {
		t.Errorf("the pass reached %d targets with no production environment recorded, want none", calls)
	}
	checks, err := driftdetector.LastChecks(ctx, s.own, "")
	if err != nil || len(checks) != 0 {
		t.Errorf("LastChecks = %+v, %v, want nothing written", checks, err)
	}
}

func TestNoServicesIsNothingToCheckAndWritesNothing(t *testing.T) {
	ctx, s, token := newStores(t)
	dir := t.TempDir()
	credential := secretref.MustNew("deploy.local")
	prj, err := project.NewWriter(s.factory, token).Create(ctx, testActor, "default")
	if err != nil {
		t.Fatalf("creating the project: %v", err)
	}
	if _, err := environment.NewWriter(s.factory, token).Create(ctx, testActor, environment.Spec{
		Kind:       environment.KindProduction,
		ProjectID:  prj.ID,
		Name:       environment.ProductionName,
		Targets:    []environment.Target{{Address: dir}},
		Credential: credential,
		Platform:   environment.Platform{Name: "local", Credential: credential, CanComposeOnDemand: true},
	}); err != nil {
		t.Fatalf("creating the production environment: %v", err)
	}
	calls := 0
	targetAt := func(dir string) targetseam.Target { calls++; return localtarget.New(dir) }

	out := &strings.Builder{}
	if err := pass(ctx, s, out, credential, targetAt); err != nil {
		t.Fatalf("pass: %v", err)
	}
	if !strings.Contains(out.String(), "no services") {
		t.Errorf("the report does not say there are no services:\n%s", out)
	}
	if calls != 0 {
		t.Errorf("the pass reached %d targets with no service recorded, want none", calls)
	}
	checks, err := driftdetector.LastChecks(ctx, s.own, "")
	if err != nil || len(checks) != 0 {
		t.Errorf("LastChecks = %+v, %v, want nothing written", checks, err)
	}
}

// factoryCounts is the row count of every table the independence rule
// covers: deploy, release, and the decision log — what a gate, a mint, or a
// page would have written had the pass been evidence about the software
// rather than a read of it.
type factoryCounts struct{ deploy, release, decisionLog int }

func countFactoryRows(ctx context.Context, t *testing.T, pool *pgxpool.Pool) factoryCounts {
	t.Helper()
	var c factoryCounts
	for table, dest := range map[string]*int{
		deploy.Table:      &c.deploy,
		release.Table:     &c.release,
		decisionlog.Table: &c.decisionLog,
	} {
		if err := pool.QueryRow(ctx, `select count(*) from `+table).Scan(dest); err != nil {
			t.Fatalf("counting %s: %v", table, err)
		}
	}
	return c
}

// TestThePassWritesNothingIntoTheFactorysStore is the independence rule
// checked rather than asserted in a comment: nothing the drift detector writes is
// evidence about the software, so a pass that raises a mismatch still leaves
// the factory's own tables exactly as they were.
func TestThePassWritesNothingIntoTheFactorysStore(t *testing.T) {
	ctx, s, token := newStores(t)
	dir := t.TempDir()
	env, svc, credential := setUp(ctx, t, s.factory, token, dir)
	shipRelease(ctx, t, s.factory, token, svc, env, "c1")
	recordRunning(t, dir, testServiceName, "bl_somebodyelses")

	before := countFactoryRows(ctx, t, s.factory)
	targetAt := func(dir string) targetseam.Target { return localtarget.New(dir) }
	if err := pass(ctx, s, &strings.Builder{}, credential, targetAt); err != nil {
		t.Fatalf("pass: %v", err)
	}
	after := countFactoryRows(ctx, t, s.factory)
	if before != after {
		t.Errorf("the factory's row counts moved from %+v to %+v", before, after)
	}
}

// TestAnOpenWindowExcusesABuildRunningBesideTheCurrentRelease is the exception
// [excusedBuilds] reads: a build the release under watch names is a mismatch
// only where no open window accounts for it.
func TestAnOpenWindowExcusesABuildRunningBesideTheCurrentRelease(t *testing.T) {
	ctx, s, token := newStores(t)
	dir := t.TempDir()
	env, svc, credential := setUp(ctx, t, s.factory, token, dir)
	shipRelease(ctx, t, s.factory, token, svc, env, "c1")
	rolling := startRelease(ctx, t, s.factory, token, svc, env, "c2")

	openWindowOver(ctx, t, s.factory, token, svc.ID, rolling, dir, 3600)
	recordRunning(t, dir, testServiceName, rolling.BuildID)

	out := &strings.Builder{}
	targetAt := func(dir string) targetseam.Target { return localtarget.New(dir) }
	if err := pass(ctx, s, out, credential, targetAt); err != nil {
		t.Fatalf("pass: %v", err)
	}

	if !strings.Contains(out.String(), "analysis window accounts for") {
		t.Errorf("the report does not say the window excuses it:\n%s", out)
	}
	held, why, err := driftdetector.NewStore(s.own).Mismatch(ctx, svc.ID)
	if err != nil || held {
		t.Errorf("Mismatch = %v %q, %v; a build an open window accounts for is excused", held, why, err)
	}
}

// openWindowOver opens an analysis window over one deploy with the parameters
// every window this test opens carries, the cap being what each test varies.
func openWindowOver(ctx context.Context, t *testing.T, pool *pgxpool.Pool, token lease.Token,
	serviceID string, over deploy.Deploy, dir string, capSeconds float64) {
	t.Helper()
	if _, err := window.NewWriter(pool, token).Open(ctx, testActor, window.OpenEvent{
		DeployID:               over.ID,
		ReleaseID:              over.ReleaseID,
		BuildID:                over.BuildID,
		ServiceID:              serviceID,
		PassedAvailable:        true,
		Size:                   map[gatepolicy.Quantity]float64{gatepolicy.QuantityErrorRate: 0.1},
		Power:                  map[gatepolicy.Quantity]float64{gatepolicy.QuantityErrorRate: 0.8},
		Confidence:             0.95,
		CapSeconds:             capSeconds,
		BoundaryVersion:        boundary.Version,
		Targets:                []string{dir},
		EmissionVersionRelease: "emission/1",
		PolicyVersion:          "pv_1",
		ScoreVersion:           "sv_1",
	}); err != nil {
		t.Fatalf("opening the window: %v", err)
	}
}

// TestAWindowOpenPastItsCapExcusesNothing is the exemption's own bound: the
// record that suppresses the check is written by the component whose failure the
// check exists to survive, so an exemption may not outlive the window it
// describes.
func TestAWindowOpenPastItsCapExcusesNothing(t *testing.T) {
	ctx, s, token := newStores(t)
	dir := t.TempDir()
	env, svc, credential := setUp(ctx, t, s.factory, token, dir)
	shipRelease(ctx, t, s.factory, token, svc, env, "c1")
	rolling := startRelease(ctx, t, s.factory, token, svc, env, "c2")

	// A cap of a thousandth of a second is past by the time the pass runs.
	openWindowOver(ctx, t, s.factory, token, svc.ID, rolling, dir, 0.001)
	recordRunning(t, dir, testServiceName, rolling.BuildID)

	out := &strings.Builder{}
	targetAt := func(dir string) targetseam.Target { return localtarget.New(dir) }
	if err := pass(ctx, s, out, credential, targetAt); err != nil {
		t.Fatalf("pass: %v", err)
	}
	held, _, err := driftdetector.NewStore(s.own).Mismatch(ctx, svc.ID)
	if err != nil {
		t.Fatalf("Mismatch: %v", err)
	}
	if !held {
		t.Errorf("a window open past its cap excused a build running beside the current release:\n%s", out)
	}
}

// TestATargetTheDeployRecordMarksCompleteIsNeverExempt is the other bound: that
// target is meant to run the release under watch, so a build the window would
// otherwise excuse is a mismatch there whatever window is open. It reads the
// exemption itself rather than a pass's report, because what the recorded
// release is per target is the reading pass.go states as an open point.
func TestATargetTheDeployRecordMarksCompleteIsNeverExempt(t *testing.T) {
	ctx, s, token := newStores(t)
	dir := t.TempDir()
	env, svc, _ := setUp(ctx, t, s.factory, token, dir)
	shipRelease(ctx, t, s.factory, token, svc, env, "c1")
	rolling := startRelease(ctx, t, s.factory, token, svc, env, "c2")
	openWindowOver(ctx, t, s.factory, token, svc.ID, rolling, dir, 3600)

	excused, err := excusedBuilds(ctx, s.factory, svc.ID)
	if err != nil {
		t.Fatalf("excusedBuilds: %v", err)
	}
	if !excused[dir][rolling.BuildID] {
		t.Fatalf("a target the rollout has not reached excuses nothing, and the exemption covers exactly those")
	}

	if err := deploy.NewWriter(s.factory, token).CompleteTarget(ctx, rolling.ID, dir,
		targetseam.ReplacementDrained); err != nil {
		t.Fatalf("completing the target: %v", err)
	}
	if excused, err = excusedBuilds(ctx, s.factory, svc.ID); err != nil {
		t.Fatalf("excusedBuilds: %v", err)
	}
	if excused[dir][rolling.BuildID] {
		t.Error("a target the deploy record marks complete was exempted")
	}
}
