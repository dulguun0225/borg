// The install's three records and the targets a service runs on: what a
// composition creates or reads before anything else, and which of production's
// addresses every read of what is running is performed against.
package main

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/project"
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

// productionAddresses is the addresses of production's targets, in the
// environment's order. It is what every read of what is running is performed
// against: a release is current when its deploy is marked complete on every one
// of them, so a producer deployed to three targets of four is not current and
// holds its consumer until the fourth lands.
func (p *path) productionAddresses() []string {
	addresses := make([]string, 0, len(p.production.Targets))
	for _, target := range p.production.Targets {
		addresses = append(addresses, target.Address)
	}
	return addresses
}
