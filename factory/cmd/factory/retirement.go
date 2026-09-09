package main

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/screens"
	"github.com/dulguun0225/borg/factory/service"
)

// What ends a service and what ends a project, and the removal each calls for,
// composed here because the deployer is: package policy writes retired on the
// service record and calls the removal, reaching no deploy target itself.

// removeService is [policy.Factory.Removal]: the deployer ends every instance of
// the service on every target of a persistent environment and writes a deploy
// record per environment naming no release, which is what makes the service's
// current release nothing wherever it ran.
//
// environmentID bounds it to one environment, which is the removal an owner has
// performed before an environment may be withdrawn; empty is every persistent
// environment, which is what a retirement reaches. The environments this install
// knows are production's alone — a customer's is a record nothing here creates —
// so an environment named that is not production's is refused rather than
// reaching nothing and reading as a removal that happened.
//
// caller is the owner whose write called for the removal, carried from the
// entrance to seam 4: the deploy record's actor is the deployer, which is what
// wrote it, and the principal on the call is who asked.
func (p *path) removeService(ctx context.Context, caller principal.Principal,
	serviceID, environmentID string) error {
	svc, err := p.serviceOf(ctx, serviceID)
	if err != nil {
		return err
	}
	if environmentID != "" && environmentID != p.production.ID {
		return fmt.Errorf("factory: this install knows production's environment alone, and the removal names %s", environmentID)
	}
	_, err = deploy.Remove(ctx, p.deploys, deploy.Removal{
		Actor:       deployActor,
		Principal:   caller,
		ServiceID:   svc.ID,
		ServiceName: svc.Name,
		From: []deploy.Environment{{
			EnvironmentID: p.production.ID,
			Credential:    p.d.credential,
			Reaches:       p.reaches(p.production, svc),
			Targets:       environmentTargets(p.production),
		}},
	})
	return err
}

// retire is the owner's write and what it reports. The removal runs after the
// version and the field commit, which package policy performs, so a removal that
// stopped leaves a service retired whose removal is performed again.
func (p *path) retire(ctx context.Context, actor record.Actor, svc service.Service) error {
	binding, _, err := p.contracts.Binding(ctx, svc.ID)
	if err != nil {
		return err
	}
	naming, dependingOn, err := unmergedItemsNaming(ctx, p, svc.ID)
	if err != nil {
		return err
	}
	version, err := p.factory.RetireService(ctx, actor, svc.ID, len(binding), naming, dependingOn)
	if err != nil {
		return fmt.Errorf("%w\n  %d consumer contract(s) in force name it, %d unmerged item(s) name it, and %d unmerged item(s) depend on one of its items",
			err, len(binding), naming, dependingOn)
	}
	fmt.Fprintf(p.d.out, "Service %s is retired by %s; policy version %s\n", svc.Name, actor.Key, version.ID)
	fmt.Fprintln(p.d.out, "  the deployer removed it from every persistent environment, so it has no current release and every reader of that record reads it as such")
	fmt.Fprintln(p.d.out, "  every record of it stays, and the repository and the store are the owner's to remove, as creating them was")
	return nil
}

// endProject is the write that ends a project and withdraws production's
// environment for it. The count of services still holding a current release on
// that environment is read here — package environment refuses the withdrawal on
// it and may not count them — and it is what a removal takes off, so retiring
// each service is what makes this write possible.
func (p *path) endProject(ctx context.Context, actor record.Actor, projectID string) error {
	running, err := servicesWithACurrentRelease(ctx, p, projectID)
	if err != nil {
		return err
	}
	version, err := p.factory.EndProject(ctx, actor, projectID, running)
	if err != nil {
		return fmt.Errorf("%w\n  %d service(s) still have a current release on production's environment; retiring each is what removes it",
			err, running)
	}
	fmt.Fprintf(p.d.out, "Project %s is ended by %s; policy version %s\n", projectID, actor.Key, version.ID)
	fmt.Fprintln(p.d.out, "  production's environment for it is withdrawn in the same write, the pairing that created the two")
	return nil
}

// removeFromEnvironment is the removal performed for one environment and
// nothing else: the service record is not written, so the service still stands
// and may still be deployed. It is what makes an environment's withdrawal
// possible, and the withdrawal is a second act of the owner's.
func (p *path) removeFromEnvironment(ctx context.Context, actor record.Actor,
	svc service.Service, environmentName string) error {
	env, found, err := environment.ByName(ctx, p.d.pool, p.projectID, environmentName)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: no environment named %q in this project", screens.ErrNotFound, environmentName)
	}
	if !env.Kind.Persistent() {
		return fmt.Errorf("factory: %s is a candidate's environment, which is torn down and not withdrawn", env.Name)
	}
	version, err := p.factory.RemoveFromEnvironment(ctx, actor, svc.ID, env.ID)
	if err != nil {
		return err
	}
	fmt.Fprintf(p.d.out, "Service %s is removed from environment %s by %s; policy version %s\n",
		svc.Name, env.Name, actor.Key, version.ID)
	fmt.Fprintln(p.d.out, "  the service record is unwritten: it still stands, and the withdrawal of the environment is the owner's next act")
	return nil
}

// unmergedItemsNaming is two of the three counts an owner's retirement is
// refused on: how many unmerged items name the service, and how many unmerged
// items of any service declare a dependency on an item of this one. An item that
// has ended — merged, dropped, or superseded — is neither.
func unmergedItemsNaming(ctx context.Context, p *path, serviceID string) (naming, dependingOn int, err error) {
	every, err := item.All(ctx, p.d.pool)
	if err != nil {
		return 0, 0, err
	}
	of := map[string]bool{}
	for _, it := range every {
		if it.ServiceID == serviceID {
			of[it.ID] = true
		}
	}
	for _, it := range every {
		switch it.Stage {
		case item.StageMerged, item.StageDropped, item.StageSuperseded:
			continue
		}
		if it.ServiceID == serviceID {
			naming++
			continue
		}
		for _, on := range it.WaitsOn {
			if of[on] {
				dependingOn++
				break
			}
		}
	}
	return naming, dependingOn, nil
}

// servicesWithACurrentRelease is how many services still have a current release
// on production's environment, which is the count package environment refuses a
// withdrawal on. A removal, complete on every target and naming no release, is
// what makes a service's current release nothing.
func servicesWithACurrentRelease(ctx context.Context, p *path, projectID string) (int, error) {
	services, err := service.All(ctx, p.d.pool)
	if err != nil {
		return 0, err
	}
	running := 0
	for _, svc := range services {
		if svc.ProjectID != projectID {
			continue
		}
		_, found, err := deploy.Current(ctx, p.d.pool, svc.ID, p.production.ID,
			serviceAddresses(p.production, svc))
		if err != nil {
			return 0, err
		}
		if found {
			running++
		}
	}
	return running, nil
}
