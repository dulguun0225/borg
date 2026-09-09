package policy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/principal"
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
		settingsID := record.NewID(factorysettings.IDPrefix)
		_, err = f.append(ctx, write{
			caller: CallerFactory, actor: actor, action: ActionCreated,
			minted: Created{Scope: Scope{Kind: ScopeFactorySettings, ID: settingsID}},
			apply: func(ctx context.Context, tx pgx.Tx) error {
				settings, err = factorysettings.Insert(ctx, tx, f.token, actor, settingsID)
				return err
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
	projectID := record.NewID(project.IDPrefix)
	productionID := record.NewID(environment.IDPrefix)
	var created Project
	version, err := f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionCreated,
		scope:    Scope{Kind: ScopeProject, ID: name},
		keyExtra: strings.Join(targets, "\n") + "\n" + credential.Name(),
		minted:   Created{Scope: Scope{Kind: ScopeProject, ID: projectID}},
		apply: func(ctx context.Context, tx pgx.Tx) error {
			proj, err := project.Insert(ctx, tx, f.token, actor, projectID, name)
			if err != nil {
				return err
			}
			envTargets := make([]environment.Target, len(targets))
			for n, address := range targets {
				envTargets[n] = environment.Target{Address: address}
			}
			production, err := environment.Insert(ctx, tx, f.token, actor, productionID, environment.Spec{
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
				return err
			}
			created = Project{Project: proj, Production: production}
			return nil
		},
	})
	if err != nil || created.Project.ID != "" {
		return created, version, err
	}
	// A step taken again wrote nothing, and the project the first performance
	// wrote is what the version in force names.
	created, err = f.projectAndProduction(ctx, version.Scope.ID)
	return created, version, err
}

// projectAndProduction is one project and the production environment written
// with it, which is what a repeated creation reads back.
func (f *Factory) projectAndProduction(ctx context.Context, projectID string) (Project, error) {
	proj, err := project.Get(ctx, f.pool, projectID)
	if err != nil {
		return Project{}, err
	}
	production, found, err := environment.Production(ctx, f.pool, projectID)
	if err != nil {
		return Project{}, err
	}
	if !found {
		return Project{}, fmt.Errorf("policy: project %s has no production environment", projectID)
	}
	return Project{Project: proj, Production: production}, nil
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
	id := record.NewID(environment.IDPrefix)
	var created environment.Environment
	version, err := f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionCreated,
		scope:    Scope{Kind: ScopeEnvironment, ID: spec.Name},
		keyExtra: string(spec.Kind) + "\n" + spec.ProjectID + "\n" + spec.Credential.Name(),
		minted:   Created{Scope: Scope{Kind: ScopeEnvironment, ID: id}},
		apply: func(ctx context.Context, tx pgx.Tx) error {
			var err error
			created, err = environment.Insert(ctx, tx, f.token, actor, id, spec)
			return err
		},
	})
	if err != nil || created.ID != "" {
		return created, version, err
	}
	created, err = environment.Get(ctx, f.pool, version.Scope.ID)
	return created, version, err
}

// RemoveFromEnvironment is the deployer's removal performed for one
// environment, which is what an owner has done before [Factory.WithdrawEnvironment]
// will take an environment other than production: the removal ends every
// instance of the service on every target of that environment and writes a
// deploy record for it naming no release, which is what makes the count
// [Factory.WithdrawEnvironment] is refused on nothing.
//
// It is the same removal [Factory.RetireService] calls and not a second one,
// bounded to the environment named — a retirement passes no environment and
// reaches every persistent one. A factory composed with no deployer refuses it
// with [ErrNoDeployer], for the reason a retirement does.
//
// It appends a version, every owner write at Factory being one, naming the
// environment the removal was performed for and the service it reached. The
// version authors nothing: what the removal writes is a deploy record, whose
// writer is the deployer, so the version records that an owner called for it
// and changes no authored value.
//
// The removal runs after the version commits, the order [Factory.RetireService]
// takes for the same reason, and it runs whether or not the version was
// appended: a step taken again writes no second version and performs the
// removal again, which is what finishes one that stopped.
func (f *Factory) RemoveFromEnvironment(ctx context.Context, actor record.Actor,
	serviceID, environmentID string) (Version, error) {
	if err := ownerOnly(actor); err != nil {
		return Version{}, err
	}
	if f.Removal == nil {
		return Version{}, fmt.Errorf("%w: %s", ErrNoDeployer, serviceID)
	}
	if environmentID == "" {
		return Version{}, ErrEnvironmentIDEmpty
	}
	version, err := f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionRemoved,
		scope: Scope{Kind: ScopeEnvironment, ID: environmentID, Key: serviceID},
	})
	if err != nil {
		return Version{}, err
	}
	if err := f.Removal(ctx, principal.Principal{Actor: actor}, serviceID, environmentID); err != nil {
		return version, fmt.Errorf("policy: removing %s from environment %s: %w", serviceID, environmentID, err)
	}
	return version, nil
}

// ErrEnvironmentIDEmpty is returned by [Factory.RemoveFromEnvironment] for a
// call naming no environment. The removal it performs is the one bounded to a
// single environment, and a call naming none would reach every persistent one,
// which is a retirement's removal and not this.
var ErrEnvironmentIDEmpty = errors.New("policy: the environment the removal is performed for is empty")

// WithdrawEnvironment ends one a customer defined, and production's as part of
// a project ending. completeDeployRecords is the count of deploy records on it
// marking a target complete for a release, which the caller read: package
// environment refuses the withdrawal where it is not nothing, and this package
// may not count them. [Factory.RemoveFromEnvironment] is what makes that count
// nothing, and it is a second act of the owner's rather than something this
// performs: the order is remove and then withdraw, and the refusal here is what
// says the first has not happened.
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
		parameter: gatepolicy.MaxConcurrentCandidateEnvironments,
		scope:     Scope{Kind: ScopeEnvironment, ID: productionID},
		number:    float64(count), authored: true,
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
	id := record.NewID(area.IDPrefix)
	var declared area.Area
	version, err := f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionCreated,
		scope:    Scope{Kind: ScopeArea, ID: name},
		keyExtra: inside.AreaID + "\n" + inside.ProjectID + "\n" + string(hazard.Grade) + "\n" + hazard.Operation,
		minted:   Created{Scope: Scope{Kind: ScopeArea, ID: id}},
		apply: func(ctx context.Context, tx pgx.Tx) error {
			var err error
			declared, err = area.Insert(ctx, tx, f.token, actor, id, name, inside, hazard)
			return err
		},
	})
	if err != nil || declared.ID != "" {
		return declared, version, err
	}
	declared, err = area.Get(ctx, f.pool, version.Scope.ID)
	return declared, version, err
}
