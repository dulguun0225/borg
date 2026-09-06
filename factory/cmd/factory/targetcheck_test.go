// The deployer's own last check per production target: what it says about
// whether a further pass is owed, which is what tells a component that stopped
// from a rollout that has finished.
package main

import (
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/lastcheck"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// TestTheDeployersLastCheckOwesAFurtherPassOnlyWhereTheRolloutHasNotFinished is
// the promise the record carries. A record past its interval with a further pass
// owed is always something that stopped, so a target the rollout has reached
// records that this pass was the last one owed there — otherwise every completed
// run would leave a record that goes stale on its own and raises a
// stale-component mismatch holding every service on that target. A target the
// rollout has not reached still owes one, which is what the drift detector's
// rollout exemption is bounded by.
func TestTheDeployersLastCheckOwesAFurtherPassOnlyWhereTheRolloutHasNotFinished(t *testing.T) {
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

	reached, found, err := lastcheck.Get(ctx, d.pool, lastcheck.ComponentDeployer, d.dir)
	if err != nil || !found {
		t.Fatalf("Get(the deployer's check on the reached target) = found %v, %v", found, err)
	}
	if reached.FurtherPassOwed() {
		t.Error("the deployer promised a further pass over a target its rollout has finished with, and nothing here makes one")
	}
	if stale, err := reached.Stale(longAfter); err != nil || stale {
		t.Errorf("Stale a day later = %v, %v; a record owed no further pass never goes stale", stale, err)
	}

	owed, found, err := lastcheck.Get(ctx, d.pool, lastcheck.ComponentDeployer, unreached)
	if err != nil || !found {
		t.Fatalf("Get(the deployer's check on the target the rollout has not reached) = found %v, %v", found, err)
	}
	if !owed.FurtherPassOwed() {
		t.Error("the deployer owes no further pass over a target its rollout has not reached")
	}
	if stale, err := owed.Stale(longAfter); err != nil || !stale {
		t.Errorf("Stale a day later = %v, %v; a rollout that stopped part way is what this reads as", stale, err)
	}
}
