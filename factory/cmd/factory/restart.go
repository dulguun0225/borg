package main

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/window"
)

// RunningBuild is what the deployer's restart asks each target of a record it
// stopped in the middle of: which build that target is running for the service
// the record names. It is [deploy.Reading].
//
// It is here and not in package deploy because reaching a target takes the
// service's name and the environment's credential, and a deploy record read
// back carries neither — it names a service by id, and the credential is the
// environment record's, which this composition holds.
func (p *path) RunningBuild(ctx context.Context, d deploy.Deploy, address string) (string, error) {
	svc, err := p.serviceOf(ctx, d.ServiceID)
	if err != nil {
		return "", err
	}
	running, err := p.d.targets.at(address).ReadRunning(ctx, deployerPrincipal, svc.Name, p.d.credential)
	if err != nil {
		return "", err
	}
	return running.Build, nil
}

// Rebuild is [deploy.Rebuilding] for the restart: what nothing about a
// stopped record holds on its own — the live seams, the credential and the
// artifact a slow return path verifies — assembled the way [path.reaches] and
// [path.RollBack] already assemble it for a fresh deploy and an ordinary
// rollback.
//
// Found is false where the build this record deploys no longer has an
// artifact on the target's directory to redeploy: [deploy.Resume] then takes
// the return path over the same seams rather than finishing the record
// forward. A service run on no target of production, or a build this
// install's own record does not name, cannot be carried either way, which is
// reported as found so [deploy.Resume] leaves the record exactly as it found
// it rather than attempting a return path with nothing to verify a redeploy
// against.
func (p *path) Rebuild(ctx context.Context, d deploy.Deploy) (deploy.Rebuilt, bool, error) {
	svc, err := p.serviceOf(ctx, d.ServiceID)
	if err != nil {
		return deploy.Rebuilt{}, true, err
	}
	addresses := serviceAddresses(p.production, svc)
	if len(addresses) == 0 {
		return deploy.Rebuilt{}, true, nil
	}
	made, err := build.Get(ctx, p.d.pool, d.BuildID)
	if err != nil {
		return deploy.Rebuilt{}, true, nil
	}

	performance := deploy.Performance{
		Actor:              deployActor,
		Principal:          deployerPrincipal,
		ServiceID:          svc.ID,
		ServiceName:        svc.Name,
		EnvironmentID:      p.production.ID,
		Credential:         p.d.credential,
		WayInAddress:       p.d.wayInAddress,
		Reaches:            p.reaches(p.production, svc),
		EnvironmentTargets: environmentTargets(p.production),
	}
	rebuilt := deploy.Rebuilt{
		Performance:    performance,
		Artifacts:      artifactsOf{dir: addresses[0]},
		RecordedDigest: made.ArtifactDigest,
	}
	if _, err := rebuilt.Artifacts.Digest(ctx, d.BuildID); err != nil {
		// The build's artifact is gone: this deploy's own release can no
		// longer be put on a target, but what was current before it still
		// can be verified and returned to.
		return rebuilt, false, nil
	}
	return rebuilt, true, nil
}

// restart is every component's restart, run once at the end of [compose] and
// before the path it composed reads a record. [compose] is its one caller, so a
// subcommand that composes a path runs it — serve, run, watch and contracts —
// and a subcommand that reaches the store through withPool or opens the pool
// itself composes no component and runs none.
//
// ../../../end-goal/one-process.md gives each of them and they are a read of
// each component's own records rather than anything kept between runs: the
// merge queue reads master and writes the release record its own unfinished
// merge left owing; the deployer completes or returns the deploy records no
// target has finished, which is the two dispositions [deploy.Resume] has; the health monitor evaluates again every window the
// deploy records left open; the notifier delivers again per row still waiting;
// Factory reads the newest policy version per scope and rewrites every
// authored field that version names which does not already hold what it names;
// and the People declaration's derived rows are rewritten the same way. Dispatch's own restart is nothing it holds, and its
// re-match of the open holds is here for the same reason: a hold is a row and
// a start is a read of it.
//
// A pass that fails stops the start. Each is a read of records this process is
// about to write over, so a start that carried on past one would decide
// against a half-finished picture of what the last one did.
func (p *path) restart(ctx context.Context) error {
	d := p.d

	for _, name := range d.serviceNames() {
		svc, found, err := service.ByName(ctx, d.pool, name)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		master, completed, err := p.queue.Restart(ctx, svc.ID)
		if err != nil {
			return err
		}
		if master.CompletedItemID != "" {
			fmt.Fprintf(d.out, "The merge queue's restart wrote the release its own unfinished merge of %s left owing\n",
				master.CompletedItemID)
		}
		if master.Stopped != "" {
			fmt.Fprintf(d.out, "The merge queue's restart found %s held: %s (wait row %s)\n",
				svc.Name, master.Stopped, master.WaitRow)
		}
		if len(completed) > 0 {
			fmt.Fprintf(d.out, "  %d outcome(s) written by that reading\n", len(completed))
		}
	}

	if err := deploy.Resume(ctx, p.deploys, p, p); err != nil {
		return err
	}
	// Resume decides every record it reads: complete, failed at a named step,
	// or returned. What is left started here is a backfill whose copy has not
	// finished, or a record a partial return left with something still owed
	// and nothing more this pass can do about it — both stand until a later
	// restart, or the copy, carries them further.
	stillStarted, err := deploy.Unfinished(ctx, d.pool)
	if err != nil {
		return err
	}
	for _, one := range stillStarted {
		fmt.Fprintf(d.out, "The deployer's restart left deploy %s standing started, with nothing further to do this pass\n", one.ID)
	}

	// The health monitor's restart is the set of windows the deploy records
	// left open, each evaluated again: an exit a stop interrupted partway is
	// finished by that second evaluation. It is the window read and not a whole
	// pass — the reading after a window has closed is what a pass does next and
	// is no part of a restart, and making it here would record a crossing on
	// every start.
	for _, name := range d.serviceNames() {
		svc, found, err := service.ByName(ctx, d.pool, name)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		open, err := window.CountOpen(ctx, d.pool, svc.ID)
		if err != nil {
			return err
		}
		if open == 0 {
			continue
		}
		watched, err := p.healthMonitor.Watch(ctx, healthmonitor.Watching{
			ID: svc.ID, Name: svc.Name, EnvironmentID: p.production.ID,
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(d.out, "The health monitor's restart evaluated %d open window(s) of %s again\n",
			len(watched), svc.Name)
		for _, one := range watched {
			p.reportWatched(one)
		}
	}

	if p.notifier != nil {
		delivered, err := p.notifier.Resume(ctx, p.d.driftdetector)
		if err != nil {
			return err
		}
		if len(delivered) > 0 {
			fmt.Fprintf(d.out, "The notifier's restart delivered %d row(s) still waiting again\n", len(delivered))
		}
	}

	rederived, err := p.factory.Rederive(ctx, p.human)
	if err != nil {
		return err
	}
	for _, one := range rederived {
		fmt.Fprintf(d.out, "Factory's restart rewrote %s to what the newest policy version names\n", one.Value.Parameter)
	}

	restored, err := people.Rederive(ctx, d.pool, d.token, p.policy, asPrincipal(p.human))
	if err != nil {
		return err
	}
	for _, key := range restored {
		fmt.Fprintf(d.out, "People's restart rewrote the declaration of %s to what the newest policy version names\n", key)
	}

	lifted, err := p.dispatch.Rematch(ctx)
	if err != nil {
		return err
	}
	if len(lifted) > 0 {
		fmt.Fprintf(d.out, "Dispatch's re-match lifted %d hold(s) whose condition is gone\n", len(lifted))
	}
	return nil
}
