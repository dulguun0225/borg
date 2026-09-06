package main

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/project"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/service"
)

// installed is the three records every composition works over: the
// factory-wide settings record, the project, and production's environment for
// it. A composition that installs creates the ones that are missing; every
// other reads them by name and refuses where the project or its production
// environment does not exist, naming the subcommand that creates them.
func (p *path) installed(ctx context.Context) (policy.Installed, error) {
	if p.d.install {
		return p.factory.Install(ctx, p.human, p.d.project,
			[]string{p.d.dir}, p.d.credential, p.d.candidateCeiling)
	}
	settings, err := factorysettings.Get(ctx, p.d.pool)
	if err != nil {
		return policy.Installed{}, fmt.Errorf(
			"factory: this factory has no factory-wide settings record; `factory run -project %s` installs one: %w",
			p.d.project, err)
	}
	prj, found, err := project.ByName(ctx, p.d.pool, p.d.project)
	if err != nil {
		return policy.Installed{}, err
	}
	if !found {
		return policy.Installed{}, fmt.Errorf(
			"factory: no project is named %q; `factory run -project %s` creates it with production's environment for it",
			p.d.project, p.d.project)
	}
	production, found, err := environment.Production(ctx, p.d.pool, prj.ID)
	if err != nil {
		return policy.Installed{}, err
	}
	if !found {
		return policy.Installed{}, fmt.Errorf("factory: project %s has no production environment", prj.ID)
	}
	version, err := p.policy.Newest(ctx, asPrincipal(p.human))
	if err != nil {
		return policy.Installed{}, err
	}
	return policy.Installed{
		Settings: settings, Project: prj, Production: production, Version: version,
	}, nil
}

// runsOnProduction authors production's targets on a service that names none,
// which is an owner's write and is made as the owner at this terminal: -targets
// is what this interface calls production's addresses, and a service installed
// here runs on all of them. It is authored once — a service already naming
// targets is left as it is, an owner having said which it runs on.
//
// It matters beyond the record saying so. What is running is read per target
// against the addresses a deploy recorded its completion on, so a service naming
// none is a service whose current deploy no reader can find: the analysis window
// stands the environment in for the whole set at the open, and the reading after
// a window has closed has no such fallback and would find nothing running.
func (p *path) runsOnProduction(ctx context.Context, svc service.Service) (service.Service, error) {
	addresses := p.production.Addresses()
	if len(svc.Targets) > 0 || len(addresses) == 0 {
		return svc, nil
	}
	if _, err := p.factory.SetServiceTargets(ctx, p.human, svc.ID, addresses, addresses); err != nil {
		return service.Service{}, err
	}
	read, found, err := service.ByName(ctx, p.d.pool, svc.Name)
	if err != nil {
		return service.Service{}, err
	}
	if !found {
		return svc, nil
	}
	return read, nil
}

// provisioned marks a service provisioned where nobody has, which is an owner's
// write and is made as the owner at this terminal, beside [path.runsOnProduction]:
// the repository is the one -service named, which this install was told about
// and which exists before a service record is written for it, and the store on
// the persistent environment is the target directory production names. It is
// written once — a service already marked is left as it is, an owner having
// said so.
//
// It matters because a service nobody has marked provisioned holds at both
// deploy rows, that hold being what stops the factory reaching for a repository
// or a store that is not there. An install on a repository host that issues
// credentials writes this through package policy with the credentials that host
// gave, and never here.
func (p *path) provisioned(ctx context.Context, svc service.Service) (service.Service, error) {
	if svc.Provisioned.Written() {
		return svc, nil
	}
	if _, err := p.factory.MarkServiceProvisioned(ctx, p.human, svc.ID,
		service.ShapeOne, repositoryCredential(), secretref.Ref{}); err != nil {
		return service.Service{}, err
	}
	read, err := service.Get(ctx, p.d.pool, svc.ID)
	if err != nil {
		return service.Service{}, err
	}
	return read, nil
}

// serviceTargets is the set every reader of targets reads: the targets of this
// environment the service record says it runs on, in the order that record
// names them, and every target of the environment where it names none — which
// is what an unwritten field means. An address the record names that the
// environment does not hold is carried as a target serving no share, which is
// the safe direction of the two and the reading
// [environment.Environment.EveryTargetServesAShare] already gives it.
func serviceTargets(env environment.Environment, svc service.Service) []environment.Target {
	if len(svc.Targets) == 0 {
		return env.Targets
	}
	set := make([]environment.Target, 0, len(svc.Targets))
	for _, address := range svc.Targets {
		target := environment.Target{Address: address}
		for _, held := range env.Targets {
			if held.Address == address {
				target = held
				break
			}
		}
		set = append(set, target)
	}
	return set
}

// serviceAddresses is [serviceTargets]' addresses. It is what every read of
// what is running is performed against: a release is current when its deploy is
// marked complete on every target the service runs on, so a producer deployed
// to three of the four it runs on is not current and holds its consumer until
// the fourth lands.
func serviceAddresses(env environment.Environment, svc service.Service) []string {
	targets := serviceTargets(env, svc)
	addresses := make([]string, 0, len(targets))
	for _, target := range targets {
		addresses = append(addresses, target.Address)
	}
	return addresses
}

// addressesOf is [serviceAddresses] for a caller holding a service's id and not
// its record.
func (p *path) addressesOf(ctx context.Context, serviceID string) ([]string, error) {
	svc, err := p.serviceOf(ctx, serviceID)
	if err != nil {
		return nil, err
	}
	return serviceAddresses(p.production, svc), nil
}
