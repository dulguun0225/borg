package deploy_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/deploy"
)

func TestKeptFleetsCountsStandingDeployRecords(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_kept"
	firstRelease := mintRelease(t, ctx, pool, token, serviceID)
	secondRelease := mintRelease(t, ctx, pool, token, serviceID)

	first, err := w.Start(ctx, deployer, deploy.Beginning{
		ServiceID: serviceID, EnvironmentID: productionID,
		What: deploy.OfRelease(firstRelease.ID, firstRelease.BuildID), Targets: twoTargets,
		IntoProduction: true, StrategyPicked: deploy.StrategyWithControl,
	})
	if err != nil {
		t.Fatalf("starting first deploy: %v", err)
	}
	if _, err := w.Start(ctx, deployer, deploy.Beginning{
		ServiceID: serviceID, EnvironmentID: productionID,
		What: deploy.OfRelease(secondRelease.ID, secondRelease.BuildID), Targets: twoTargets,
		IntoProduction: true, StrategyPicked: deploy.StrategyWithControl,
	}); err != nil {
		t.Fatalf("starting second deploy: %v", err)
	}

	count, err := deploy.KeptFleets(ctx, pool, serviceID, productionID)
	if err != nil {
		t.Fatalf("KeptFleets: %v", err)
	}
	if count != 2 {
		t.Fatalf("KeptFleets = %d, want two standing deploy records", count)
	}

	for _, address := range addressesOf(twoTargets) {
		if err := w.TearDownKept(ctx, first.ID, address, 1, deploy.Priced{}); err != nil {
			t.Fatalf("tearing down kept fleet at %s: %v", address, err)
		}
	}
	count, err = deploy.KeptFleets(ctx, pool, serviceID, productionID)
	if err != nil {
		t.Fatalf("KeptFleets after teardown: %v", err)
	}
	if count != 1 {
		t.Errorf("KeptFleets after teardown = %d, want one standing deploy record", count)
	}
}
