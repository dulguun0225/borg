package policy

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/project"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/service"
)

// Installed is what [Factory.Install] found or created.
type Installed struct {
	Settings   factorysettings.Settings
	Project    project.Project
	Production environment.Environment
	Version    Version
}

// Install is the records that exist before any parameter is authored: the
// factory-wide settings record, which exists before any project does, and the
// first project with production's environment for it. Each appends a policy
// version, so a factory that has been installed has a version in force with
// nothing authored — which is what a gate names when an owner has authored
// nothing at all.
//
// The production environment's platform is "local", composed on demand, through
// the credential given — the one implementation this milestone has.
//
// It is idempotent. Running it against a factory that has every record appends
// no version and returns what is there, so the command-line interface may call
// it at every start.
func (f *Factory) Install(ctx context.Context, actor record.Actor, projectName string,
	targets []string, credential secretref.Ref, candidateCeiling int) (Installed, error) {
	if err := ownerOnly(actor); err != nil {
		return Installed{}, err
	}

	settings, err := factorysettings.Get(ctx, f.pool)
	if errors.Is(err, factorysettings.ErrNotFound) {
		_, err = f.append(ctx, write{
			caller: CallerFactory, actor: actor, action: ActionCreated,
			mint: func(ctx context.Context, tx pgx.Tx) (Created, error) {
				settings, err = factorysettings.Insert(ctx, tx, f.token, actor)
				return Created{Scope: Scope{Kind: ScopeFactorySettings, ID: settings.ID}}, err
			},
		})
	}
	if err != nil {
		return Installed{}, err
	}

	proj, found, err := project.ByName(ctx, f.pool, projectName)
	if err != nil {
		return Installed{}, err
	}
	if found {
		production, found, err := environment.Production(ctx, f.pool, proj.ID)
		if err != nil {
			return Installed{}, err
		}
		if !found {
			return Installed{}, fmt.Errorf("policy: project %s has no production environment", proj.ID)
		}
		version, err := f.newest(ctx, actor)
		if err != nil {
			return Installed{}, err
		}
		return Installed{Settings: settings, Project: proj, Production: production, Version: version}, nil
	}

	created, version, err := f.CreateProject(ctx, actor, projectName, targets, credential)
	if err != nil {
		return Installed{}, err
	}
	if candidateCeiling > 0 {
		version, err = f.SetMaxConcurrentCandidateEnvironments(ctx, actor, created.Production.ID, candidateCeiling)
		if err != nil {
			return Installed{}, err
		}
	}
	return Installed{Settings: settings, Project: created.Project, Production: created.Production, Version: version}, nil
}

// Project is a project and the production environment created with it.
type Project struct {
	Project    project.Project
	Production environment.Environment
}

// CreateProject writes a project and production's environment for it in the
// same event, which is what the record inventory gives that write, and appends
// one version naming the project. An owner does not choose whether production
// exists: every gate row of the default path reads production's environment,
// which is there before the item is.
func (f *Factory) CreateProject(ctx context.Context, actor record.Actor, name string,
	targets []string, credential secretref.Ref) (Project, Version, error) {
	var created Project
	version, err := f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionCreated,
		scope: Scope{Kind: ScopeProject, ID: name},
		mint: func(ctx context.Context, tx pgx.Tx) (Created, error) {
			proj, err := project.Insert(ctx, tx, f.token, actor, name)
			if err != nil {
				return Created{}, err
			}
			envTargets := make([]environment.Target, len(targets))
			for n, address := range targets {
				envTargets[n] = environment.Target{Address: address}
			}
			production, err := environment.Insert(ctx, tx, f.token, actor, environment.Spec{
				Kind:       environment.KindProduction,
				ProjectID:  proj.ID,
				Name:       environment.ProductionName,
				Targets:    envTargets,
				Credential: credential,
				Platform: environment.Platform{
					Name:               "local",
					Credential:         credential,
					CanComposeOnDemand: true,
				},
			})
			if err != nil {
				return Created{}, err
			}
			created = Project{Project: proj, Production: production}
			return Created{Scope: Scope{Kind: ScopeProject, ID: proj.ID}}, nil
		},
	})
	return created, version, err
}

// EndProject ends one project and withdraws its production environment in the
// same write, which is the pairing that created the two. A project is ended
// once every service in it is retired: the services in it are counted here,
// this package being a reader of that record already, and package project
// refuses the write where the count is not nothing.
//
// completeDeployRecords is the count of deploy records on production's
// environment marking a target complete for a release, which the caller read:
// package environment refuses the withdrawal where it is not nothing, and this
// package may not count them. Every service's removal is what makes it nothing.
func (f *Factory) EndProject(ctx context.Context, actor record.Actor,
	projectID string, completeDeployRecords int) (Version, error) {
	services, err := service.All(ctx, f.pool)
	if err != nil {
		return Version{}, err
	}
	standing := 0
	for _, svc := range services {
		if svc.ProjectID == projectID && !svc.Retired() {
			standing++
		}
	}
	production, found, err := environment.Production(ctx, f.pool, projectID)
	if err != nil {
		return Version{}, err
	}
	if !found {
		return Version{}, fmt.Errorf("policy: project %s has no production environment", projectID)
	}
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionWithdrawn,
		scope: Scope{Kind: ScopeProject, ID: projectID},
		apply: func(ctx context.Context, tx pgx.Tx) error {
			if err := project.End(ctx, tx, f.token, actor, projectID, standing); err != nil {
				return err
			}
			return environment.Withdraw(ctx, tx, f.token, actor, production.ID, completeDeployRecords)
		},
	})
}

// AuthorStrategyDefault authors the rollout strategy production takes where
// nothing narrows the pick. It is production's environment record alone: a
// strategy decides whether a control runs, and a control is a comparison
// against organic traffic, which no other kind has.
func (f *Factory) AuthorStrategyDefault(ctx context.Context, actor record.Actor,
	productionID string, strategy gatepolicy.Strategy) (Version, error) {
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionAuthored,
		parameter: gatepolicy.StrategyDefault,
		scope:     Scope{Kind: ScopeEnvironment, ID: productionID},
		list:      []string{string(strategy)}, authored: true,
		apply: func(ctx context.Context, tx pgx.Tx) error {
			return environment.SetStrategyDefault(ctx, tx, f.token, actor, productionID, strategy)
		},
	})
}

// CreateEnvironment writes an environment a customer defines. Production's is
// not written here: it is written with the project, in the same event.
func (f *Factory) CreateEnvironment(ctx context.Context, actor record.Actor,
	spec environment.Spec) (environment.Environment, Version, error) {
	var created environment.Environment
	version, err := f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionCreated,
		scope: Scope{Kind: ScopeEnvironment, ID: spec.Name},
		mint: func(ctx context.Context, tx pgx.Tx) (Created, error) {
			var err error
			created, err = environment.Insert(ctx, tx, f.token, actor, spec)
			if err != nil {
				return Created{}, err
			}
			return Created{Scope: Scope{Kind: ScopeEnvironment, ID: created.ID}}, nil
		},
	})
	return created, version, err
}

// WithdrawEnvironment ends one a customer defined, and production's as part of
// a project ending. completeDeployRecords is the count of deploy records on it
// marking a target complete for a release, which the caller read: package
// environment refuses the withdrawal where it is not nothing, and this package
// may not count them.
//
// It takes no gate row: what a gate row decides is a withdrawal that removes a
// human from a gate, and this removes an environment.
func (f *Factory) WithdrawEnvironment(ctx context.Context, actor record.Actor,
	environmentID string, completeDeployRecords int) (Version, error) {
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionWithdrawn,
		scope: Scope{Kind: ScopeEnvironment, ID: environmentID},
		apply: func(ctx context.Context, tx pgx.Tx) error {
			return environment.Withdraw(ctx, tx, f.token, actor, environmentID, completeDeployRecords)
		},
	})
}

// SetMaxConcurrentCandidateEnvironments authors how many candidate environments
// the platform may hold at once, a field of the production environment record
// beside the platform it declares. It is authored outright with nothing
// supplied, and it is not gate policy — it is a version because every owner
// write at Factory is one.
func (f *Factory) SetMaxConcurrentCandidateEnvironments(ctx context.Context, actor record.Actor,
	productionID string, count int) (Version, error) {
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionAuthored,
		scope:  Scope{Kind: ScopeEnvironment, ID: productionID, Key: "max_concurrent_candidate_environments"},
		number: float64(count),
		apply: func(ctx context.Context, tx pgx.Tx) error {
			return environment.SetMaxConcurrentCandidateEnvironments(ctx, tx, f.token, actor, productionID, count)
		},
	})
}

// DeclareArea writes an area with the hazard severity an owner declared on it,
// which is authored outright with nothing supplied: nothing the factory
// observes says what harm the software can do.
func (f *Factory) DeclareArea(ctx context.Context, actor record.Actor, name string,
	inside area.Inside, hazard area.Hazard) (area.Area, Version, error) {
	var declared area.Area
	version, err := f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionCreated,
		scope: Scope{Kind: ScopeArea, ID: name},
		mint: func(ctx context.Context, tx pgx.Tx) (Created, error) {
			var err error
			declared, err = area.Insert(ctx, tx, f.token, actor, name, inside, hazard)
			if err != nil {
				return Created{}, err
			}
			return Created{Scope: Scope{Kind: ScopeArea, ID: declared.ID}}, nil
		},
	})
	return declared, version, err
}
