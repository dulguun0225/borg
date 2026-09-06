package main

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// How the deployer performs one deploy on this platform: the targets in the
// environment's order, the principal every call at seam 4 is made as, and the
// two shapes the path deploys in — one build onto a candidate's own environment,
// and one release onto production's targets under the strategy the row picked.

// deployerPrincipal is who the calls at seam 4 are made as: the deployer, as
// itself. The fleet entry an agent would call as is not built and no agent
// reaches a deploy target at all, so this is the one principal that crosses this
// seam.
var deployerPrincipal = principal.OfComponent("deployer")

// reaches is the targets of one environment the service runs on, as the
// deployer reaches them: [serviceTargets] in that set's own order, each with the
// target this platform reaches that address through and what the record
// declares about it. The rollout's order is over the service's set and not over
// the environment's whole list, so a project whose services sit on three subsets
// of one environment rolls each out over its own.
//
// No instances are kept: this platform moves a process rather than traffic, so
// there is no second fleet to keep and a rollback is a redeploy of a binary
// still on disk.
func (p *path) reaches(env environment.Environment, svc service.Service) []deploy.Reach {
	targets := serviceTargets(env, svc)
	reaching := make([]deploy.Reach, 0, len(targets))
	for _, target := range targets {
		reaching = append(reaching, deploy.Reach{
			Address:      target.Address,
			Target:       p.d.targets.at(target.Address),
			ServesAShare: target.ServesAShare,
		})
	}
	return reaching
}

// intoCandidate puts one build on a candidate's own environment: one target, the
// directory that environment names, and no strategy at all — a strategy attaches
// to a production deploy and to no other, there being no organic traffic on a
// candidate to compare two builds against.
//
// The value set and the schema changes are empty. Neither is derived anywhere in
// this interface yet: what a build's resolved value set is and what changes it
// declares are the component that built's, and nothing here reads either, so the
// record's configuration digest is over nothing and the store's history is left
// where it is.
func (p *path) intoCandidate(ctx context.Context, c *candidate, buildID string) (deploy.Deploy, error) {
	return deploy.Perform(ctx, p.deploys, deploy.Performance{
		Actor:         deployActor,
		Principal:     deployerPrincipal,
		ServiceID:     c.svc.ID,
		ServiceName:   c.svc.Name,
		EnvironmentID: c.environmentID,
		What:          deploy.OfBuild(buildID),
		Credential:    p.d.credential,
		Reaches: []deploy.Reach{{
			Address: c.environmentDir,
			Target:  p.d.targets.at(c.environmentDir),
		}},
	})
}

// intoProduction puts the release on the production targets the service runs on,
// in that set's order, one at a time, under the strategy the production deploy
// row picked. The bake volume between one target and the next is zero and no
// [deploy.Bake] is supplied, so the deployer holds nowhere: what could answer it
// is the health monitor reading the window this deploy has not opened yet, and
// package deploy's doc.go says that caller is not built.
func (p *path) intoProduction(ctx context.Context, c *candidate, pick gate.Pick) (deploy.Deploy, error) {
	return deploy.Perform(ctx, p.deploys, deploy.Performance{
		Actor:          deployActor,
		Principal:      deployerPrincipal,
		ServiceID:      c.svc.ID,
		ServiceName:    c.svc.Name,
		EnvironmentID:  p.production.ID,
		What:           deploy.OfRelease(c.releaseID, c.reverifiedBuildID),
		IntoProduction: true,
		StrategyPicked: strategyOf(pick),
		Credential:     p.d.credential,
		Reaches:        p.reaches(p.production, c.svc),
	})
}

// strategyOf is the row's pick as the deploy record names it. The two
// vocabularies are two packages' and neither imports the other, so the crossing
// is here, at the one place a pick read off an open event becomes a field of a
// deploy record. A pick naming nothing is the row without a control: every
// production deploy names one, and a firing that picked none picked the row that
// takes all of the traffic.
func strategyOf(pick gate.Pick) deploy.Strategy {
	if pick.Strategy == gate.StrategyWithControl {
		return deploy.StrategyWithControl
	}
	return deploy.StrategyWithoutControl
}

// adopt writes the deployer's four fields on the service record after a deploy
// onto a persistent environment: the target answered, what it reports running,
// whether an earlier build is there to return to, and whether the service emits
// what the health monitor reads.
//
// The fourth is the one the deployer cannot see, so it is read here: the
// emission on this platform is the file the started process writes, and whether
// it holds anything is what says the health monitor has something to read. A
// build deployed a moment ago has written nothing yet, so a first release reads
// as no emission and the next deploy of that service writes the field again.
func (p *path) adopt(ctx context.Context, svc service.Service, dep deploy.Deploy) error {
	targets := serviceTargets(p.production, svc)
	if len(targets) == 0 {
		return nil
	}
	address := targets[0].Address
	running, err := p.d.targets.at(address).ReadRunning(ctx, deployerPrincipal, svc.Name, p.d.credential)
	if err != nil {
		return err
	}
	units, _, err := countSignal(localtarget.SignalFile(address, dep.BuildID))
	if err != nil {
		return err
	}
	found := deploy.Found(running.Instances, rollbackPathPresent(running), units > 0)
	if err := deploy.Adopt(ctx, p.deploys, deployActor, svc.ID, found); err != nil {
		return err
	}
	fmt.Fprintf(p.d.out, "The deployer wrote what it found on service %s: %d instance(s) running, a rollback path %v, an emission %v\n",
		svc.Name, running.Instances, found.RollbackPathPresent, found.EmissionReadable)
	return nil
}

// rollbackPathPresent is whether there is an earlier build on the target to
// return to. On this platform every build a deploy ever placed stays in the
// directory, so the path is present as soon as the target answers with a build
// at all — and a directory pruned between the deploy and the rollback is what
// would make that false, which nothing here prunes.
func rollbackPathPresent(running targetseam.Running) bool { return running.Build != "" }
