package main

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/window"
)

// restart is every component's restart, run once at every start of this
// process before anything else reads a record.
//
// ../../../end-goal/one-process.md gives each of them and they are a read of
// each component's own records rather than anything kept between runs: the
// merge queue reads master and writes the release record its own unfinished
// merge left owing; the deployer completes or returns the deploy records no
// target has finished; the health monitor evaluates again every window the
// deploy records left open; the notifier delivers again per row still waiting;
// Factory rewrites any owner-authored field the newest policy version per
// scope no longer names; and the People declaration's derived rows are
// rewritten the same way. Dispatch's own restart is nothing it holds, and its
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

	resumed, err := deploy.Resume(ctx, p.deploys)
	if err != nil {
		return err
	}
	for _, one := range resumed {
		fmt.Fprintf(d.out, "The deployer's restart finished deploy %s, which no target had completed\n", one.ID)
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
		delivered, err := p.notifier.Resume(ctx)
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
