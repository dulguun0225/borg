package healthmonitor

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/window"
)

// Kept is one fleet a rollback needs: the instances of a release, kept at the
// capacity they had times the fraction its owner authored, on one target of one
// deploy. They are what a rollback shifts traffic onto, and they are torn down
// when the last window that could return to that release closes and never at an
// exit of their own.
type Kept struct {
	ServiceID     string
	ServiceName   string
	EnvironmentID string
	// DeployID is the deploy record that names them — the deploy that replaced
	// the release these instances run — and Target the address they run on.
	DeployID string
	Target   string
	// OfReleaseID is the release whose instances these are: the rollback's
	// target computed for the release that deploy delivered.
	OfReleaseID string
}

// tearDownKept ends every kept fleet this service still runs that nothing could
// return to any more. It runs after a window closes, because whether this was
// the last window that could return to a release is a question only the close
// answers.
//
// It reads every window of the service and not only the one just closed: a
// window that closed earlier while this one was still open left its kept fleet
// standing, and nothing comes back to it afterwards.
//
// Two things keep a fleet standing, and each is read off a record. A window
// still open could return to it, whatever exit it eventually takes; a window
// that has closed could not, at any of the four exits, which is why a closed
// one is a candidate here rather than a guard. And the release production is
// currently running is never torn down, whatever its windows did — which is
// what leaves the instances a rollback has just shifted traffic onto standing.
func (h *HealthMonitor) tearDownKept(ctx context.Context, w Watching) error {
	if h.deployer == nil {
		return nil
	}
	all, err := window.All(ctx, h.pool, w.ID)
	if err != nil {
		return err
	}
	couldReturn := map[string]bool{}
	for _, one := range all {
		if !one.Open() {
			continue
		}
		target, has, err := h.returnsTo(ctx, w, one)
		if err != nil {
			return err
		}
		if has {
			couldReturn[target.ID] = true
		}
	}

	svc, err := service.Get(ctx, h.pool, w.ID)
	if err != nil {
		return err
	}
	current, running, err := deploy.Current(ctx, h.pool, w.ID, w.EnvironmentID, targetsOrDefault(svc.Targets, w))
	if err != nil {
		return err
	}
	if running {
		couldReturn[current.ReleaseID] = true
	}

	for _, one := range all {
		if one.Open() {
			continue
		}
		target, has, err := h.returnsTo(ctx, w, one)
		if err != nil {
			return err
		}
		if !has || couldReturn[target.ID] {
			continue
		}
		if err := h.endKeptOf(ctx, w, one, target); err != nil {
			return err
		}
	}
	return nil
}

// returnsTo is the release a rollback of the window's own release would return
// to, whose instances that window's deploy keeps. A search's window names no
// release and keeps nothing: what its control runs is the rollback's target,
// which the search never tears down.
func (h *HealthMonitor) returnsTo(ctx context.Context, w Watching, one window.Window) (release.Release, bool, error) {
	if one.ReleaseID == "" {
		return release.Release{}, false, nil
	}
	rel, err := release.Get(ctx, h.pool, one.ReleaseID)
	if err != nil {
		return release.Release{}, false, err
	}
	return h.TargetBelow(ctx, w, rel.Number)
}

// endKeptOf asks the deployer to end the kept fleet on every target of that
// window's deploy that still names one standing. The deploy record is what
// names them per target — how many are kept there and whether they have been
// torn down — so a repeat asks for nothing.
func (h *HealthMonitor) endKeptOf(ctx context.Context, w Watching, one window.Window, target release.Release) error {
	targets, err := deploy.Targets(ctx, h.pool, one.DeployID)
	if err != nil {
		return err
	}
	for _, t := range targets {
		if t.Fleets.Kept.Instances == 0 || t.Fleets.Kept.TornDownAt != "" {
			continue
		}
		kept := Kept{
			ServiceID: w.ID, ServiceName: w.Name, EnvironmentID: w.EnvironmentID,
			DeployID: one.DeployID, Target: t.Address, OfReleaseID: target.ID,
		}
		if err := h.deployer.TearDownKept(ctx, kept); err != nil {
			return fmt.Errorf("healthmonitor: tearing down the fleet kept for %s on %s: %w",
				w.Name, t.Address, err)
		}
	}
	return nil
}
