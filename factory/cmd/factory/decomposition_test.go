// Decomposition reaching a service record that already exists rather than
// creating a second one.
package main

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/service"
)

// TestDecompositionReachesAnExistingService writes the service record before the run
// and asserts the path reaches it rather than creating a second one. The
// service's name is unique in the store, so a decomposition that created every run
// would be refused by that constraint from the second item on that service
// onwards — a later change, or this one run again after a reject.
func TestDecompositionReachesAnExistingService(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)

	// The service record is already there — newPath writes it, the analysis window's
	// four being fields of it and having to be authored before the first window opens.
	// What this test is about is what decomposition does with one it finds.
	before, found, err := service.ByName(ctx, d.pool, theService)
	if err != nil || !found {
		t.Fatalf("reading the service before the run: found %v, %v", found, err)
	}

	res, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	if res.serviceID != before.ID {
		t.Errorf("decomposition used service %s, the record it should have reached is %s", res.serviceID, before.ID)
	}

	var services int
	if err := d.pool.QueryRow(ctx, `select count(*) from `+service.Table+` where name = $1`, theService).Scan(&services); err != nil {
		t.Fatalf("counting the services named %q: %v", theService, err)
	}
	if services != 1 {
		t.Errorf("%d services are named %q, decomposition writes a service's identity once", services, theService)
	}
}

// TestDecompositionRefusesAServiceWhoseProductionPlatformCannotComposeOnDemand:
// an environment per candidate is the shape the design admits and nothing
// else, so decomposition refuses to write an item for a service whose
// project's production environment declares a platform that cannot compose
// one on demand, and not only where that record was created.
func TestDecompositionRefusesAServiceWhoseProductionPlatformCannotComposeOnDemand(t *testing.T) {
	ctx, d, _ := newPath(t, "")
	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path: %v", err)
	}

	const otherProject = "prj_cccccccccccccccccccccccccccccccc"
	if _, err := environment.NewWriter(d.pool, d.token).Create(ctx,
		owner(t, ctx, d.pool, d.token, d.human), environment.Spec{
			Kind:       environment.KindProduction,
			ProjectID:  otherProject,
			Name:       environment.ProductionName,
			Targets:    []environment.Target{{Address: t.TempDir()}},
			Credential: d.credential,
			Platform: environment.Platform{
				Name:               "local",
				Credential:         d.credential,
				CanComposeOnDemand: false,
			},
		}); err != nil {
		t.Fatalf("creating the second project's production: %v", err)
	}

	const otherService = "other-service"
	if _, err := service.NewWriter(d.pool, d.token).Create(ctx, decompositionActor,
		otherService, t.TempDir(), otherProject); err != nil {
		t.Fatalf("creating the second project's service: %v", err)
	}

	in := intent.Intent{ID: "in_second"}
	requirements := []agent.Requirement{{ID: "rq_second", Statement: "test"}}
	if _, err := p.decomposeItems(ctx, in, []string{otherService}, requirements); !errors.Is(err, environment.ErrCannotCompose) {
		t.Errorf("decomposing a service whose production platform cannot compose on demand = %v, want ErrCannotCompose", err)
	}
}

// TestDecompositionLeavesNoServiceBehindWhenTheItemWriteFails is C0717 and
// C0718: the service record decomposition creates and the item naming it are
// one write, so a failure writing the item leaves no service record behind
// either. The item write is refused here by answering no requirement at all,
// which [item.Decomposition.Create] refuses on its own terms.
func TestDecompositionLeavesNoServiceBehindWhenTheItemWriteFails(t *testing.T) {
	ctx, d, _ := newPath(t, "")
	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path: %v", err)
	}

	// A service the run was never told about beforehand, so it does not exist
	// when decomposeItems reaches for it — the branch [service.ByName] answers
	// false on, unlike theService the fixture already wrote.
	const newService = "brand-new-service"
	p.d.services = append(p.d.services, serviceRepo{name: newService, repo: t.TempDir()})

	in := intent.Intent{ID: "in_no_requirement"}
	if _, err := p.decomposeItems(ctx, in, []string{newService}, nil); !errors.Is(err, item.ErrAnswersNoRequirement) {
		t.Fatalf("decomposing with no requirement answered = %v, want item.ErrAnswersNoRequirement", err)
	}

	if _, found, err := service.ByName(ctx, d.pool, newService); err != nil {
		t.Fatalf("reading the service after the failed decomposition: %v", err)
	} else if found {
		t.Errorf("service %q stands even though the item write naming it failed", newService)
	}
}
