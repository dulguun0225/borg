// The deployer's own last check per target of a persistent environment, and
// its own last check per platform, both written on every production deploy.
package main

import (
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/lastcheck"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// TestTheDeployersLastCheckIsWrittenPerTargetAndAlwaysOwesAFurtherPass is the
// promise the record carries. A rollout advances only while the deployer
// runs, so a target whose deployer last check is past the interval it names,
// with a further pass owed, is what stops a drift-detection exemption
// standing on a rollout that is not advancing — and every check this write
// makes names a further pass owed, whether or not the rollout has reached
// that target yet, because the deployer keeps passing over a persistent
// target for as long as it is in the environment. That is what makes a
// rollout finished with a target and a deployer that has simply stopped
// running read the same way once the interval passes: only staleness answers
// it, and never the rollout's own progress. The record is keyed by the
// target's own address, so a target the rollout has not reached yet gets its
// own row and the target it has reached is untouched by it.
func TestTheDeployersLastCheckIsWrittenPerTargetAndAlwaysOwesAFurtherPass(t *testing.T) {
	ctx, d, _ := newPath(t, "")
	path := p(ctx, t, d)
	svc := theServiceRecord(t, ctx, path)
	unreached := t.TempDir()

	dep, err := deploy.NewWriter(d.pool, d.token).Start(ctx, deployActor, deploy.Beginning{
		ServiceID:     svc.ID,
		EnvironmentID: path.production.ID,
		What:          deploy.OfBuild(record.NewID("bld")),
		Targets: []deploy.Reaching{
			{Address: d.dir}, {Address: unreached},
		},
		IntoProduction: true,
		StrategyPicked: deploy.StrategyWithoutControl,
	})
	if err != nil {
		t.Fatalf("starting the deploy: %v", err)
	}
	if err := deploy.NewWriter(d.pool, d.token).CompleteTarget(ctx, dep.ID, d.dir,
		targetseam.ReplacementDrained); err != nil {
		t.Fatalf("completing the target the rollout reached: %v", err)
	}

	if err := path.recordTargetChecks(ctx, dep); err != nil {
		t.Fatalf("recording the deployer's last checks: %v", err)
	}

	// Long enough after that any interval this interface promises has passed.
	longAfter := time.Now().Add(24 * time.Hour)

	for _, address := range []string{d.dir, unreached} {
		check, found, err := lastcheck.Get(ctx, d.pool, lastcheck.ComponentDeployer, address)
		if err != nil || !found {
			t.Fatalf("Get(the deployer's check on %s) = found %v, %v", address, found, err)
		}
		if !check.FurtherPassOwed() {
			t.Errorf("the deployer's check on %s owes no further pass, want one owed until the target leaves the environment", address)
		}
		if stale, err := check.Stale(longAfter); err != nil || !stale {
			t.Errorf("Stale a day later on %s = %v, %v; nothing here promises a further pass and never makes one", address, stale, err)
		}
	}
}

// TestTheDeployersPlatformCheckIsWrittenOnEveryProductionDeploy is
// lastcheck.Writer.RecordPlatformPass, the sole writer of the deployer's
// per-platform record, exercised through the composition: package deploy no
// longer has a second writer of its own, and this is what calls the one that
// is left — keyed by the production environment record's own id and not by
// the platform's name, so an install whose projects run on two platforms adds
// neither count across them.
func TestTheDeployersPlatformCheckIsWrittenOnEveryProductionDeploy(t *testing.T) {
	ctx, d, _ := newPath(t, "")
	path := p(ctx, t, d)

	if err := path.recordPlatformCheck(ctx); err != nil {
		t.Fatalf("recording the deployer's platform check: %v", err)
	}

	check, found, err := lastcheck.Get(ctx, d.pool, lastcheck.ComponentDeployer, path.production.ID)
	if err != nil || !found {
		t.Fatalf("Get(the deployer's check on the platform) = found %v, %v", found, err)
	}
	pass, err := lastcheck.PlatformPassOf(check)
	if err != nil {
		t.Fatalf("PlatformPassOf: %v", err)
	}
	if pass.StandingByTheRecords != 0 || pass.HeldByThePlatform != 0 {
		t.Errorf("the pass reads %+v, want no candidate environments standing on a fresh install", pass)
	}
}
