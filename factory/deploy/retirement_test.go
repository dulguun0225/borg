// The removal: what the deployer performs when an owner retires a service, one
// deploy record per persistent environment naming no release.
package deploy_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// TestARemovalEndsEveryInstanceAndWritesARecordPerEnvironment: the write that
// retires a service calls the deployer, which ends every instance of it on every
// target of every persistent environment and writes a deploy record per
// environment naming no release — which is what makes the service's current
// release nothing wherever it ran.
func TestARemovalEndsEveryInstanceAndWritesARecordPerEnvironment(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)
	reaches, fakes := twoFakes(false)

	live, err := deploy.Perform(ctx, w, performance(serviceID, r, reaches))
	if err != nil {
		t.Fatalf("Perform: %v", err)
	}
	if _, found, err := deploy.Current(ctx, pool, serviceID, productionID, addressesOf(twoTargets)); err != nil || !found {
		t.Fatalf("Current after the deploy = found %v, %v", found, err)
	}
	if live.Status != deploy.StatusComplete {
		t.Fatalf("the deploy is %q, want it complete before the removal", live.Status)
	}

	staging, stagingFakes := twoFakes(false)
	removals, err := deploy.Remove(ctx, w, deploy.Removal{
		Actor:       deployer,
		Principal:   deployerCalls,
		ServiceID:   serviceID,
		ServiceName: "checkout",
		From: []deploy.Environment{
			{EnvironmentID: productionID, Credential: credential, Reaches: reaches},
			{EnvironmentID: "env_staging", Credential: credential, Reaches: staging},
		},
	})
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(removals) != 2 {
		t.Fatalf("the removal wrote %d records, want one per persistent environment", len(removals))
	}
	for _, removed := range removals {
		if removed.ReleaseID != "" || removed.BuildID != "" {
			t.Errorf("the removal's record names %+v, want no release and no build", removed)
		}
		if removed.Status != deploy.StatusComplete {
			t.Errorf("the removal on %s is %q, want it complete as the instances there end",
				removed.EnvironmentID, removed.Status)
		}
	}

	for _, fake := range append(fakes, stagingFakes...) {
		stopped := false
		for _, call := range fake.Calls() {
			if call.Op == targetseam.OpStop {
				stopped = true
			}
		}
		if !stopped {
			t.Errorf("a target was asked %+v, want every instance of the service ended there", fake.Calls())
		}
	}

	// Current release is read from the newest complete deploy record, and a
	// removal's names none, so a retired service has no current release.
	if _, found, err := deploy.Current(ctx, pool, serviceID, productionID, addressesOf(twoTargets)); err != nil || found {
		t.Errorf("Current after the removal = found %v, %v, want none", found, err)
	}
}

// TestARemovalNamesTheServiceAndSomewhereToPerformIt: a removal reaching no
// environment ends nothing, and one naming no service names nothing to end.
func TestARemovalNamesTheServiceAndSomewhereToPerformIt(t *testing.T) {
	ctx, _, w, _ := newTableWithToken(t)
	reaches, _ := twoFakes(false)

	asking := deploy.Removal{
		Actor: deployer, Principal: deployerCalls, ServiceID: "svc_a", ServiceName: "checkout",
		From: []deploy.Environment{{EnvironmentID: productionID, Credential: credential, Reaches: reaches}},
	}

	nowhere := asking
	nowhere.From = nil
	if _, err := deploy.Remove(ctx, w, nowhere); !errors.Is(err, deploy.ErrRemovalIncomplete) {
		t.Errorf("a removal reaching no environment = %v, want ErrRemovalIncomplete", err)
	}
	unnamed := asking
	unnamed.ServiceName = ""
	if _, err := deploy.Remove(ctx, w, unnamed); !errors.Is(err, deploy.ErrRemovalIncomplete) {
		t.Errorf("a removal naming no service = %v, want ErrRemovalIncomplete", err)
	}
}
