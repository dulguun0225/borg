// The mitigation: what Ops asks the deployer to perform on a target outside a
// rollout, on a human's instruction, and what the drift detector reads as
// intended state that differs from the deploy record on purpose.
package deploy_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// opsHuman is the named human at Ops whose instruction the deployer performs a
// mitigation on. The actor under seam 1 is that human and never the deployer.
var opsHuman = record.Actor{Kind: record.KindHuman, Key: "ada", Basis: record.BasisClaimed}

// TestAMitigationIsPerformedOnAHumansInstructionAndStandsUntilItIsEnded: the
// record is written before the call, so a call that stops halfway leaves a
// mitigation standing rather than an operation nothing recorded, and it is what
// the drift detector reads as intended state until it is ended.
func TestAMitigationIsPerformedOnAHumansInstructionAndStandsUntilItIsEnded(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)
	reaches, fakes := twoFakes(true)

	d, err := deploy.Perform(ctx, w, performance(serviceID, r, reaches))
	if err != nil {
		t.Fatalf("Perform: %v", err)
	}

	m, err := deploy.Mitigate(ctx, w, deploy.Mitigating{
		Actor: opsHuman, Principal: deployerCalls, Operation: deploy.OperationSetInstanceCount,
		Address: "/srv/one", Target: fakes[0], DeployID: d.ID,
		ServiceName: "checkout", Build: r.BuildID, Count: 6, Credential: credential,
	})
	if err != nil {
		t.Fatalf("Mitigate: %v", err)
	}
	if !m.Standing() || m.Actor.Key != opsHuman.Key {
		t.Errorf("the mitigation is %+v, want one standing, named by the human who asked for it", m)
	}

	asked := false
	for _, call := range fakes[0].Calls() {
		if call.Op == targetseam.OpSetInstanceCount && call.Count == 6 {
			asked = true
		}
	}
	if !asked {
		t.Errorf("the seam was asked %+v, want the instance count the mitigation names", fakes[0].Calls())
	}

	standing, err := deploy.StandingMitigations(ctx, pool)
	if err != nil {
		t.Fatalf("StandingMitigations: %v", err)
	}
	if len(standing) != 1 || standing[0].ID != m.ID || standing[0].DeployID != d.ID {
		t.Fatalf("the standing mitigations are %+v, want the one just performed against its deploy record", standing)
	}

	if err := w.EndMitigation(ctx, m.ID); err != nil {
		t.Fatalf("EndMitigation: %v", err)
	}
	standing, err = deploy.StandingMitigations(ctx, pool)
	if err != nil {
		t.Fatalf("StandingMitigations: %v", err)
	}
	if len(standing) != 0 {
		t.Errorf("%d mitigations still stand, want an ended one read as nothing", len(standing))
	}
	against, err := deploy.Mitigations(ctx, pool, d.ID)
	if err != nil {
		t.Fatalf("Mitigations: %v", err)
	}
	if len(against) != 1 || against[0].EndedAt == "" {
		t.Errorf("the mitigations of the deploy are %+v, want the ended one still on the record", against)
	}
	if err := w.EndMitigation(ctx, m.ID); !errors.Is(err, deploy.ErrMitigationNotFound) {
		t.Errorf("ending a mitigation twice = %v, want ErrMitigationNotFound", err)
	}
}

// TestAMitigationIsRefusedWhereNoHumanAskedAndWhereItNamesNothing: the deployer
// performs one on a human's instruction from Ops, so the actor under seam 1 is
// that human; and one naming no target or no deploy record would say nothing
// about what is intended where.
func TestAMitigationIsRefusedWhereNoHumanAskedAndWhereItNamesNothing(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)
	reaches, fakes := twoFakes(false)

	d, err := deploy.Perform(ctx, w, performance(serviceID, r, reaches))
	if err != nil {
		t.Fatalf("Perform: %v", err)
	}

	asking := deploy.Mitigating{
		Actor: opsHuman, Principal: deployerCalls, Operation: deploy.OperationEndEveryInstance,
		Address: "/srv/one", Target: fakes[0], DeployID: d.ID,
		ServiceName: "checkout", Credential: credential,
	}

	byTheDeployer := asking
	byTheDeployer.Actor = deployer
	if _, err := deploy.Mitigate(ctx, w, byTheDeployer); !errors.Is(err, deploy.ErrNotAHuman) {
		t.Errorf("a mitigation the deployer asked itself for = %v, want ErrNotAHuman", err)
	}
	naming := asking
	naming.DeployID = ""
	if _, err := deploy.Mitigate(ctx, w, naming); !errors.Is(err, deploy.ErrMitigationIncomplete) {
		t.Errorf("a mitigation naming no deploy record = %v, want ErrMitigationIncomplete", err)
	}
	unknown := asking
	unknown.Operation = "restart_the_world"
	if _, err := deploy.Mitigate(ctx, w, unknown); !errors.Is(err, deploy.ErrOperationUnknown) {
		t.Errorf("a mitigation naming an operation outside the class = %v, want ErrOperationUnknown", err)
	}
	if standing, err := deploy.StandingMitigations(ctx, pool); err != nil || len(standing) != 0 {
		t.Errorf("a refused mitigation was recorded: %+v, %v", standing, err)
	}
}
