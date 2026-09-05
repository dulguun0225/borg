package healthmonitor

import (
	"context"
	"fmt"
	"time"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/lastcheck"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/window"
)

// Watched is one window evaluated: the window as it stands after the reading,
// what was read, what crossed, and what followed. Every number the exit was
// reached from is on it, because a boundary nobody can recompute is one nobody
// can argue with.
type Watched struct {
	// Window is the window as it stands after the reading, so a window this
	// reading closed carries its exit.
	Window window.Window
	// Release is the release read, and Baseline is the release whose build the
	// other arm runs — the control's build, or the release below on the target
	// where the strategy kept none. HasBaseline is false where there is neither,
	// which makes both exits unreachable.
	Release     release.Release
	Baseline    release.Release
	HasBaseline bool
	// Evaluated is the comparison over every target, operation and quantity.
	Evaluated Evaluated
	// ControlCrossing is the control failing the reading against the service's
	// own recent history, and is nil where the control is readable. While it
	// stands the window cannot exit passed: a comparison rules a change out
	// relative to the control, so a control that is itself failing lets a bad
	// release pass.
	ControlCrossing *Crossing
	Exit            window.Exit
	IncidentID      string
	RaisedIntentID  string
	// Rolled is what the rollback undid, and is nil where none happened. Target
	// is the release it returned to.
	Rolled *Rollback
	Target release.Release
	// WhyNoRollback is why a failed exit rolled nothing back, and is empty
	// otherwise: no release to return to, a mismatch standing on the service, or
	// a factory composed with no deployer.
	WhyNoRollback string
	// SkippedWindows is the windows this reading closed skipped, which is every
	// open window of a release the rollback undid.
	SkippedWindows []string
}

// Watch evaluates every open window of one service, oldest first, and does what
// each exit requires. It is the window's own authority: inside the window a
// crossing rolls the release back with no human involved and no waiting.
//
// Oldest first, because that is the order a rollback skips over and the order in
// which a lower release's crossing decides the fate of the ones above it. A
// window closed skipped by an earlier reading in the same pass is not read
// again — its release is no longer running, so there is nothing to read.
//
// Each pass writes the health monitor's own last check for the service, whatever
// it found, so that a monitor that stopped is a row a reader can see rather than
// silence. It is written last, after the windows are decided, because what it
// records is that the pass ran.
func (h *HealthMonitor) Watch(ctx context.Context, w Watching) ([]Watched, error) {
	if err := w.validate(); err != nil {
		return nil, err
	}
	svc, err := service.Get(ctx, h.pool, w.ID)
	if err != nil {
		return nil, err
	}
	open, err := window.AllOpen(ctx, h.pool, w.ID)
	if err != nil {
		return nil, err
	}

	skipped := map[string]bool{}
	var watched []Watched
	for _, win := range open {
		if skipped[win.ID] {
			continue
		}
		one, err := h.read(ctx, w, svc, win)
		watched = append(watched, one)
		if err != nil {
			return watched, err
		}
		for _, id := range one.SkippedWindows {
			skipped[id] = true
		}
	}
	if err := h.recordPass(ctx, w, svc, watched); err != nil {
		return watched, err
	}
	return watched, nil
}

// read is one open window evaluated: the comparison first, then the two readings
// beside it, then the exit each answer requires.
func (h *HealthMonitor) read(ctx context.Context, w Watching, svc service.Service, win window.Window) (Watched, error) {
	one := Watched{Window: win}
	if win.MeasuresNothing {
		// The window records only that it measures nothing for this service, so
		// there is nothing to read and its cap has already run.
		closed, err := h.windows.Close(ctx, win.ID, window.ExitTimedOut, window.Closing{})
		one.Window, one.Exit = closed, window.ExitTimedOut
		return one, err
	}

	if win.ReleaseID != "" {
		rel, err := release.Get(ctx, h.pool, win.ReleaseID)
		if err != nil {
			return one, err
		}
		one.Release = rel
	}
	baseline, hasBaseline, err := h.baselineOf(ctx, w, win, one.Release)
	if err != nil {
		return one, err
	}
	one.Baseline, one.HasBaseline = baseline, hasBaseline

	if err := h.compare(ctx, w, win, baseline, hasBaseline, &one); err != nil {
		return one, err
	}
	if one.Evaluated.Crossed == nil {
		if err := h.readBeside(ctx, w, svc, win, &one); err != nil {
			return one, err
		}
	}
	if one.Evaluated.Crossed != nil {
		return h.failed(ctx, w, one)
	}

	// Passed is available only while the control is readable, and only where the
	// window had that exit at all. A comparison rules a change out relative to the
	// control, so a control that is itself failing lets a bad release pass — the
	// one reading nobody corrects afterwards, because nothing rolled back and
	// nothing is left to mark.
	if one.Evaluated.PassedEvery && win.PassedAvailable && one.ControlCrossing == nil && one.Evaluated.Volume {
		return h.close(ctx, w, win, window.ExitPassed, one)
	}

	// Neither, so the cap is what ends it: the condition a window is meant to
	// reach is a traffic volume, and what ends a window that will never reach that
	// volume cannot be.
	past, err := win.PastCap(time.Now())
	if err != nil {
		return one, err
	}
	if past {
		return h.close(ctx, w, win, window.ExitTimedOut, one)
	}
	return one, nil
}

// compare reads the comparison on every target the boundary was allocated over,
// each target's release instances against the arm beside them, and never pooled
// across targets. Pooled, a regression confined to one target is diluted by
// every target it is not on.
func (h *HealthMonitor) compare(ctx context.Context, w Watching, win window.Window,
	baseline release.Release, hasBaseline bool, into *Watched) error {
	for _, target := range win.Targets {
		reading := Reading{
			ServiceName: w.Name,
			Target:      target,
			Release:     Arm{BuildID: win.BuildID, DeployID: win.DeployID},
		}
		if hasBaseline {
			reading.Baseline = h.baselineArm(ctx, win, baseline)
		}
		series, err := h.emission.Read(ctx, reading)
		if err != nil {
			return fmt.Errorf("healthmonitor: reading %s on %s: %w", w.Name, target, err)
		}
		if err := evaluate(win.Boundary, win.Power, target, series, KindComparison, &into.Evaluated); err != nil {
			return err
		}
	}
	return nil
}

// baselineArm is the other arm of the comparison. Under a strategy that keeps a
// control it is the control the deployer started beside this release: the
// control was placed by this deploy and the long-lived instances of the same
// build by an earlier one, so the deploy is what tells the two apart. Without a
// control it is the build of the release below wherever it runs, which is the
// weak fallback and carries the confound a started control exists to remove.
func (h *HealthMonitor) baselineArm(ctx context.Context, win window.Window, baseline release.Release) Arm {
	dep, err := deploy.Get(ctx, h.pool, win.DeployID)
	if err == nil && dep.StrategyPerformed == deploy.StrategyWithControl {
		return Arm{BuildID: baseline.BuildID, DeployID: win.DeployID}
	}
	return Arm{BuildID: baseline.BuildID}
}

// baselineOf is the release whose build the other arm runs: the rollback's
// target for a window over a release, and what is current for a search's window,
// whose deploy names a build and no release and whose control is the rollback's
// target the search never tears down.
func (h *HealthMonitor) baselineOf(ctx context.Context, w Watching, win window.Window,
	rel release.Release) (release.Release, bool, error) {
	if win.ReleaseID == "" {
		current, running, err := deploy.Current(ctx, h.pool, w.ID, w.EnvironmentID, targetsOrDefault(win.Targets, w))
		if err != nil || !running || current.ReleaseID == "" {
			return release.Release{}, false, err
		}
		found, err := release.Get(ctx, h.pool, current.ReleaseID)
		return found, err == nil, err
	}
	return h.TargetBelow(ctx, w, rel.Number)
}

// targetsOrDefault is the addresses [deploy.Current] reads completion against:
// the targets on the window it was allocated over, and — where there is no
// window to read them from — the environment stands for the whole set until
// its own record is read here, which doc.go says is not built.
func targetsOrDefault(targets []string, w Watching) []string {
	if len(targets) > 0 {
		return targets
	}
	return []string{w.EnvironmentID}
}

// close is the passed and timed-out exits, which take the same order: the health
// monitor calls the deployer to tear the control down and then closes the
// window. Teardown is ordered by what a rollback needs and not by the exit
// alone, and the window's close is the last durable step of it.
func (h *HealthMonitor) close(ctx context.Context, w Watching, win window.Window,
	exit window.Exit, one Watched) (Watched, error) {
	if err := h.tearDownControls(ctx, w, win); err != nil {
		return one, err
	}
	closed, err := h.windows.Close(ctx, win.ID, exit, window.Closing{
		On:                one.Evaluated.Read,
		FinestSizeReached: one.Evaluated.FinestSizeReached,
	})
	if err != nil {
		return one, err
	}
	one.Window, one.Exit = closed, exit
	return one, nil
}

// tearDownControls ends the control on every target the window was allocated
// over. A control exists only while a window is open, and one left running after
// its window closed is a mismatch like any other, which is how a failed teardown
// is caught.
func (h *HealthMonitor) tearDownControls(ctx context.Context, w Watching, win window.Window) error {
	if h.deployer == nil {
		return nil
	}
	for _, target := range win.Targets {
		control := Control{
			ServiceID: w.ID, ServiceName: w.Name, EnvironmentID: w.EnvironmentID,
			DeployID: win.DeployID, Target: target,
		}
		if err := h.deployer.TearDownControl(ctx, control); err != nil {
			return fmt.Errorf("healthmonitor: tearing down the control for %s on %s: %w", w.Name, target, err)
		}
	}
	return nil
}

// newestRecord is the newest time the store held a record for this service over
// everything the pass read, which the last check carries so that a read whose
// newest record is older than its interval is read as no volume.
func newestRecord(watched []Watched) string {
	newest := ""
	for _, one := range watched {
		if one.Evaluated.Newest > newest {
			newest = one.Evaluated.Newest
		}
	}
	if newest == "" {
		return "none"
	}
	return newest
}

// recordPass writes the health monitor's own last check for the service: the
// interval its pass runs on, and on its last pass that no further pass is owed,
// which is what a retired service gets. Without it a stopped monitor and a
// service with nothing to report are the same row.
func (h *HealthMonitor) recordPass(ctx context.Context, w Watching, svc service.Service, watched []Watched) error {
	if h.checks == nil {
		return nil
	}
	_, err := h.checks.Record(ctx, Actor, lastcheck.LastCheck{
		Component: lastcheck.ComponentHealthMonitor,
		Subject:   w.ID,
		Interval:  h.readings.PassInterval,
		LastPass:  svc.Retired(),
		Payload:   fmt.Sprintf("%d window(s) read, newest record %s", len(watched), newestRecord(watched)),
	})
	return err
}
