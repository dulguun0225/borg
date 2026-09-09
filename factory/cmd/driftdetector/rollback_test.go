// rollback_test.go is the third exemption case's own test: [openWindows]
// assembling [driftdetector.WindowTarget.KeptBuildID] as the release a
// rollback of the release under watch would return to, on a target running no
// control. It reads openWindows and driftdetector.Excused directly rather
// than a pass's report, the way TestATargetTheDeployRecordMarksCompleteIsNeverExempt
// in pass_test.go does — a pass's own comparison already agrees with a
// target's previous release for an unreached target of its own accord, which
// is no test of this case at all. startReleaseKeeping and closeWindowOver are
// its own fixtures, beside the ones fixtures_test.go shares.
package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/window"
)

// TestTheKeptBuildFallsBackToTheReleaseARollbackWouldReturnToIsExcused is
// C1929's third case: on a target running no control, a build the window
// would otherwise treat as a mismatch is excused where it is the build of
// the release a rollback of the one under watch would return to, and that
// target's own kept instances are what makes the fallback apply at all.
func TestTheKeptBuildFallsBackToTheReleaseARollbackWouldReturnToIsExcused(t *testing.T) {
	ctx, s, token := newStores(t)
	dir := t.TempDir()
	env, svc, _ := setUp(ctx, t, s.factory, token, dir)

	first := shipRelease(ctx, t, s.factory, token, svc, env, "c1")
	closeWindowOver(ctx, t, s.factory, token, svc.ID, first, dir, window.ExitTimedOut)

	rolling := startReleaseKeeping(ctx, t, s.factory, token, svc, env, "c2", 1)
	openWindowOver(ctx, t, s.factory, token, svc.ID, rolling, dir, 3600)

	windows, err := openWindows(ctx, s.factory, svc.ID, env.ID)
	if err != nil {
		t.Fatalf("openWindows: %v", err)
	}
	// first.BuildID is neither the release under watch's build nor a control's:
	// it is excused only through the kept build, the release a rollback of the
	// one under watch would return to.
	if !driftdetector.Excused(windows, dir, first.BuildID, nil, time.Now()) {
		t.Fatalf("the release a rollback of the one under watch would return to is not excused via the kept build")
	}
}

// TestTheKeptBuildDoesNotExcuseWhereTheTargetKeepsNoInstances is the guard: a
// rollback needs instances there to return to, so a target whose kept fleet is
// nothing does not fall back to the release below the one under watch.
func TestTheKeptBuildDoesNotExcuseWhereTheTargetKeepsNoInstances(t *testing.T) {
	ctx, s, token := newStores(t)
	dir := t.TempDir()
	env, svc, _ := setUp(ctx, t, s.factory, token, dir)

	first := shipRelease(ctx, t, s.factory, token, svc, env, "c1")
	closeWindowOver(ctx, t, s.factory, token, svc.ID, first, dir, window.ExitTimedOut)

	// No kept instances named this time.
	rolling := startReleaseKeeping(ctx, t, s.factory, token, svc, env, "c2", 0)
	openWindowOver(ctx, t, s.factory, token, svc.ID, rolling, dir, 3600)

	windows, err := openWindows(ctx, s.factory, svc.ID, env.ID)
	if err != nil {
		t.Fatalf("openWindows: %v", err)
	}
	if driftdetector.Excused(windows, dir, first.BuildID, nil, time.Now()) {
		t.Error("a target keeping no instances was excused via the rollback target's build")
	}
}

// startReleaseKeeping is [startRelease] with the target's own kept instances
// named, which [openWindows] reads through the deploy target's kept fleet to
// gate the third exemption case.
func startReleaseKeeping(ctx context.Context, t *testing.T, pool *pgxpool.Pool, token lease.Token,
	svc service.Service, env environment.Environment, commitHash string, keptInstances int) deploy.Deploy {
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
		targets[n] = deploy.Reaching{Address: target.Address, KeptInstances: keptInstances}
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

// closeWindowOver opens an analysis window over one deploy the way
// openWindowOver does and closes it at exit at once, so
// [window.ClosedPassedOrTimedOut] reads it as the history a rollback target
// below the next release opened over is computed from.
func closeWindowOver(ctx context.Context, t *testing.T, pool *pgxpool.Pool, token lease.Token,
	serviceID string, over deploy.Deploy, dir string, exit window.Exit) {
	t.Helper()
	win, err := window.NewWriter(pool, token).Open(ctx, testActor, window.OpenEvent{
		DeployID:               over.ID,
		ReleaseID:              over.ReleaseID,
		BuildID:                over.BuildID,
		ServiceID:              serviceID,
		PassedAvailable:        true,
		Size:                   map[gatepolicy.Quantity]float64{gatepolicy.QuantityErrorRate: 0.1},
		Power:                  map[gatepolicy.Quantity]float64{gatepolicy.QuantityErrorRate: 0.8},
		Confidence:             0.95,
		CapSeconds:             3600,
		BoundaryVersion:        boundary.Version,
		Targets:                []string{dir},
		EmissionVersionRelease: "emission/1",
		PolicyVersion:          "pv_1",
		ScoreVersion:           "sv_1",
	})
	if err != nil {
		t.Fatalf("opening the window over %s: %v", over.ID, err)
	}
	if _, err := window.NewWriter(pool, token).Close(ctx, win.ID, exit, window.Closing{}); err != nil {
		t.Fatalf("closing the window: %v", err)
	}
}
