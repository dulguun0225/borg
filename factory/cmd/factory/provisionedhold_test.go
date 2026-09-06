// The hold a service its owner has not marked provisioned leaves at both deploy
// rows, and the owner's write that lifts it.
package main

import (
	"slices"
	"testing"

	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/service"
)

// TestAServiceNotProvisionedHoldsAtBothDeployRows: the field an owner writes
// once the repository and the store exist is what the hold reads, so a service
// record nobody has marked holds the candidate deploy and the production deploy
// alike, and the write lifts both.
func TestAServiceNotProvisionedHoldsAtBothDeployRows(t *testing.T) {
	ctx, d, _ := newPath(t, "")
	svc, found, err := service.ByName(ctx, d.pool, theService)
	if err != nil || !found {
		t.Fatalf("reading the service the fixture wrote = found %v, %v", found, err)
	}
	if svc.Provisioned.Written() {
		t.Fatal("the fixture's service is already marked provisioned, so this test would prove nothing")
	}
	// An intent and one item of it: the holds are computed for a firing on an
	// item, so there has to be one to compute them for.
	in, err := intent.NewIntake(d.pool, d.token, intent.NoNotifier{}).TakeIn(ctx, healthmonitor.Actor,
		intent.Arrival{
			Source: intent.SourceDetector, Statement: "the service is not provisioned yet",
			Evidence: intent.Evidence{ServiceID: svc.ID},
		})
	if err != nil {
		t.Fatalf("taking an intent in: %v", err)
	}
	it, err := item.NewDecomposition(d.pool, d.token).Create(ctx, decompositionActor,
		item.New{IntentID: in.ID, ServiceID: svc.ID, Branch: "item/provisioning"}, "", svc.ProjectID, nil)
	if err != nil {
		t.Fatalf("writing an item on it: %v", err)
	}

	held := p(ctx, t, d)
	for _, row := range []gate.Row{gate.DeployToCandidateEnvironment, gate.DeployToProduction} {
		standing, err := held.Standing(ctx, gate.Subjects{
			Row: row, ItemID: it.ID, ServiceID: svc.ID, EnvironmentID: held.production.ID,
		})
		if err != nil {
			t.Fatalf("the holds standing at %s: %v", row, err)
		}
		if !slices.Contains(standing, gate.HoldServiceNotProvisioned) {
			t.Errorf("the holds standing at %s are %v, want the service not provisioned among them", row, standing)
		}
	}

	if _, err := held.provisioned(ctx, svc); err != nil {
		t.Fatalf("marking the service provisioned: %v", err)
	}
	// A path of its own, because the one above read the service before the write
	// and keeps what it read.
	lifted := p(ctx, t, d)
	for _, row := range []gate.Row{gate.DeployToCandidateEnvironment, gate.DeployToProduction} {
		standing, err := lifted.Standing(ctx, gate.Subjects{
			Row: row, ItemID: it.ID, ServiceID: svc.ID, EnvironmentID: lifted.production.ID,
		})
		if err != nil {
			t.Fatalf("the holds standing at %s after the write: %v", row, err)
		}
		if slices.Contains(standing, gate.HoldServiceNotProvisioned) {
			t.Errorf("the hold still stands at %s after the owner marked the service provisioned", row)
		}
	}
}
