package deploy_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/deploy"
)

func TestProductionReachesUsesTheHealthMonitorSelectedControl(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	controlRelease := mintRelease(t, ctx, pool, token, serviceID)
	reaches, _ := twoFakes(true)
	control, err := deploy.Perform(ctx, w, performance(serviceID, controlRelease, reaches))
	if err != nil {
		t.Fatalf("the selected control deploy: %v", err)
	}
	controlTargets, err := deploy.Targets(ctx, pool, control.ID)
	if err != nil {
		t.Fatalf("the selected control targets: %v", err)
	}
	got, _, selected, err := deploy.ProductionReaches(ctx, pool, deployerCalls,
		credential, "checkout", productionID, control, true, "", reaches,
		deploy.StrategyWithControl, 0.1)
	if err != nil {
		t.Fatalf("ProductionReaches: %v", err)
	}
	if !selected || got[0].ControlInstances != controlTargets[0].Fleets.Release.Instances || got[0].KeptInstances != controlTargets[0].Fleets.Release.Instances {
		t.Fatalf("selected reach = %+v, selected=%v, want the selected control's full capacity", got[0], selected)
	}
	if got[0].Share != 0.1 {
		t.Errorf("selected reach share = %v, want 0.1 from the pick", got[0].Share)
	}
}
