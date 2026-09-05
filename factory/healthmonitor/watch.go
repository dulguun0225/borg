package healthmonitor

import (
	"context"
	"fmt"
	"time"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/window"
)

// Reading is one evaluation of one release: the window it was read under, what the
// quantity said, what the boundary made of it, and what followed. Every number the
// exit was reached from is on it, because a boundary nobody can recompute is one
// nobody can argue with.
type Reading struct {
	// Window is the window as it stands after the reading, so a window this reading
	// closed carries its exit. It is the zero window for [HealthMonitor.AfterWindow],
	// where the window has already closed and the release is read all the same.
	Window window.Window
	// Release is the release read, and Baseline is what it was read against — false
	// where there was none, which makes both exits unreachable.
	Release        release.Release
	Baseline       release.Release
	HasBaseline    bool
	Observed       boundary.Observed
	Boundary       boundary.Reading
	Exit           window.Exit
	IncidentID     string
	RaisedIntentID string
	// Rolled is what the rollback undid, and is nil where none happened. Target is
	// the release it returned to, and is the zero release where none happened.
	Rolled *Rollback
	Target release.Release
	// WhyNoRollback is why a failed exit rolled nothing back, and is empty otherwise:
	// no release to return to, or a factory whose deploy agent cannot perform one.
	WhyNoRollback string
}

// Crossing is what an incident says crossed, and it is the boundary's own rather
// than a threshold an owner stated: no explicit health threshold is authored here,
// so this is the only crossing this milestone writes.
const Crossing = "the comparison crossed its own boundary against the release"

// Watch evaluates every open window of one service, oldest first, and does what
// each exit requires. It is the window's own authority: inside the window a
// crossing rolls the release back with no human involved and no waiting.
//
// Oldest first, because that is the order a rollback sweeps and the order in which
// a lower release's crossing decides the fate of the ones above it. A window closed
// swept by an earlier reading in the same pass is not read again — its release is no
// longer running, so there is nothing to read.
func (h *HealthMonitor) Watch(ctx context.Context, w Watching) ([]Reading, error) {
	if err := w.validate(); err != nil {
		return nil, err
	}
	open, err := window.AllOpen(ctx, h.pool, w.ID)
	if err != nil {
		return nil, err
	}

	swept := map[string]bool{}
	var readings []Reading
	for _, win := range open {
		if swept[win.ID] {
			continue
		}
		reading, sweptNow, err := h.read(ctx, w, win)
		readings = append(readings, reading)
		if err != nil {
			return readings, err
		}
		for _, id := range sweptNow {
			swept[id] = true
		}
	}
	return readings, nil
}

// read is one open window evaluated. It returns the ids of any windows it closed
// swept, so the pass around it does not read a release that is no longer running.
func (h *HealthMonitor) read(ctx context.Context, w Watching, win window.Window) (Reading, []string, error) {
	reading := Reading{Window: win}

	rel, err := release.Get(ctx, h.pool, win.ReleaseID)
	if err != nil {
		return reading, nil, err
	}
	reading.Release = rel

	observed, baseline, hasBaseline, err := h.observe(ctx, w, rel)
	if err != nil {
		return reading, nil, err
	}
	reading.Observed, reading.Baseline, reading.HasBaseline = observed, baseline, hasBaseline

	b := boundary.Boundary{Size: win.Size, Confidence: win.Confidence}
	reading.Boundary, err = b.Evaluate(observed)
	if err != nil {
		return reading, nil, err
	}

	switch {
	case reading.Boundary.Harm:
		return h.failed(ctx, w, reading)
	case reading.Boundary.Clean:
		closed, err := h.windows.Close(ctx, win.ID, window.ExitPassed, observed)
		reading.Window, reading.Exit = closed, window.ExitPassed
		return reading, nil, err
	}

	// Neither, so it times out: the condition a window is meant to reach is a traffic
	// volume, and what ends a window that will never reach that volume cannot be.
	past, err := win.PastCap(time.Now())
	if err != nil {
		return reading, nil, err
	}
	if past {
		closed, err := h.windows.Close(ctx, win.ID, window.ExitTimedOut, observed)
		reading.Window, reading.Exit = closed, window.ExitTimedOut
		return reading, nil, err
	}
	return reading, nil, nil
}

// observe reads the quantity for one release against the release a rollback from
// it would return to.
func (h *HealthMonitor) observe(ctx context.Context, w Watching, rel release.Release) (boundary.Observed, release.Release, bool, error) {
	baseline, has, err := TargetBelow(ctx, h.pool, w.ID, rel.Number)
	if err != nil {
		return boundary.Observed{}, release.Release{}, false, err
	}
	q := Quantity{ServiceName: w.Name, BuildID: rel.BuildID}
	if has {
		q.BaselineBuildID = baseline.BuildID
	}
	observed, err := h.signal.Read(ctx, q)
	if err != nil {
		return boundary.Observed{}, baseline, has, fmt.Errorf(
			"healthmonitor: reading the quantity for release %d of %s: %w", rel.Number, w.Name, err)
	}
	return observed, baseline, has, nil
}

// failed is the exit with no human in it: the incident, the window closed, the
// revert intent raised, the rollback asked for, and every window the rollback swept
// closed skipped.
//
// The order is the one the design forces. The incident and the window come first,
// because they are what says the crossing happened and a rollback that failed
// halfway should not leave the crossing unrecorded. The revert intent is raised
// before the rollback is asked for, so the rollback's record can name it — and an
// intent with no rollback behind it is a state the design already has, for a
// failed exit that found no release to return to.
func (h *HealthMonitor) failed(ctx context.Context, w Watching, reading Reading) (Reading, []string, error) {
	deployed, err := deploy.Get(ctx, h.pool, reading.Window.DeployID)
	if err != nil {
		return reading, nil, err
	}
	raised, err := h.recordCrossing(ctx, w, reading.Release, deployed.ID, true)
	if err != nil {
		return reading, nil, err
	}
	reading.IncidentID, reading.RaisedIntentID = raised.IncidentID, raised.IntentID

	closed, err := h.windows.Close(ctx, reading.Window.ID, window.ExitFailed, reading.Observed)
	if err != nil {
		return reading, nil, err
	}
	reading.Window, reading.Exit = closed, window.ExitFailed

	target, has, err := TargetBelow(ctx, h.pool, w.ID, reading.Release.Number)
	if err != nil {
		return reading, nil, err
	}
	switch {
	case !has:
		// A service's first release has no target at all: nothing below it closed
		// without failing a release, no control is running under it, and there is no earlier build
		// to redeploy. The failed release keeps serving and the revert is the only
		// correction, which the intent above has already asked for.
		reading.WhyNoRollback = "no release below this one has a window that closed without failing it, so there is nothing to return to"
		return reading, nil, h.report(ctx, reading)
	case h.rollbacker == nil:
		reading.WhyNoRollback = "this factory is composed with no deploy agent to perform one"
		return reading, nil, h.report(ctx, reading)
	}

	above, err := release.Above(ctx, h.pool, w.ID, reading.Release.Number)
	if err != nil {
		return reading, nil, err
	}
	sweptIDs := make([]string, 0, len(above))
	for _, r := range above {
		sweptIDs = append(sweptIDs, r.ID)
	}

	rollback := Rollback{
		ServiceID:       w.ID,
		ServiceName:     w.Name,
		EnvironmentID:   w.EnvironmentID,
		ToReleaseID:     target.ID,
		ToBuildID:       target.BuildID,
		FailedReleaseID: reading.Release.ID,
		SweptReleaseIDs: sweptIDs,
		Source:          deploy.SourceHealthMonitorAtFailed,
		RevertIntentID:  raised.IntentID,
	}
	if err := h.rollbacker.RollBack(ctx, rollback); err != nil {
		return reading, nil, fmt.Errorf("healthmonitor: rolling release %d of %s back to %d: %w",
			reading.Release.Number, w.Name, target.Number, err)
	}
	reading.Rolled, reading.Target = &rollback, target

	// Every release the rollback undid above the failed one is swept: its
	// health monitor simply stopped, because master is linear and the release was above
	// the target. A window already closed keeps the exit it closed at — a window
	// closes once — so only the open ones are closed here.
	var closedSkipped []string
	for _, r := range above {
		win, found, err := window.ForRelease(ctx, h.pool, r.ID)
		if err != nil {
			return reading, closedSkipped, err
		}
		if !found || !win.Open() {
			continue
		}
		// Skipped takes no read: a rollback aimed below this release ended the window,
		// so there was no reading and a read stored here would be one nothing
		// performed.
		if _, err := h.windows.Close(ctx, win.ID, window.ExitSkipped, boundary.Observed{}); err != nil {
			return reading, closedSkipped, err
		}
		closedSkipped = append(closedSkipped, win.ID)
	}
	return reading, closedSkipped, h.report(ctx, reading)
}

// raisedCrossing is what recording a crossing produced: the incident it landed on,
// and the intent it raised where it raised one.
type raisedCrossing struct {
	IncidentID string
	IntentID   string
}

// recordCrossing writes the crossing as an incident, or as an observation on the
// one already open for this service and release — which is what keeps a second
// crossing from becoming a second intent. raiseIntent is what the caller wants a
// new incident to carry: a failed exit raises a revert, and a crossing after the
// window closed raises the item that investigates it.
func (h *HealthMonitor) recordCrossing(ctx context.Context, w Watching, rel release.Release,
	deployID string, revert bool) (raisedCrossing, error) {
	open, found, err := incident.Open(ctx, h.pool, w.ID, rel.ID)
	if err != nil {
		return raisedCrossing{}, err
	}
	if found {
		observed, err := h.incidents.Observe(ctx, open.ID)
		if err != nil {
			return raisedCrossing{}, err
		}
		return raisedCrossing{IncidentID: observed.ID, IntentID: observed.IntentID}, nil
	}

	statement := AfterWindowStatement(w.Name, rel.Number)
	if revert {
		statement = RevertStatement(w.Name, rel.Number)
	}
	raised, err := h.intake.TakeIn(ctx, Actor, intent.Arrival{
		Source:    intent.SourceDetector,
		Statement: statement,
		Evidence:  intent.Evidence{ServiceID: w.ID, ReleaseID: rel.ID},
	})
	if err != nil {
		return raisedCrossing{}, err
	}
	written, err := h.incidents.Raise(ctx, Actor, incident.Raising{
		EnvironmentID: w.EnvironmentID,
		ServiceID:     w.ID,
		ReleaseID:     rel.ID,
		DeployID:      deployID,
		Crossing:      Crossing,
		IntentID:      raised.ID,
	})
	if err != nil {
		return raisedCrossing{}, err
	}
	return raisedCrossing{IncidentID: written.ID, IntentID: raised.ID}, nil
}

// RevertStatement is what the health monitor writes as the revert intent's statement.
// It names the release by its number and the service by its name, because an intent
// whose statement holds an id is one no human reads — and because what refines it is
// an agent reading the statement.
func RevertStatement(serviceName string, number int64) string {
	return fmt.Sprintf("Revert release %d of %s: its analysis window closed failed, and %s.",
		number, serviceName, Crossing)
}

// AfterWindowStatement is what a crossing found after the window closed writes.
// It is an ordinary unrefined intent taking the same stages and the same gates as
// any other, so the statement is a request and not a command.
func AfterWindowStatement(serviceName string, number int64) string {
	return fmt.Sprintf("Release %d of %s is failing more of its work than the release below it: %s. Find the cause and fix it.",
		number, serviceName, Crossing)
}

// report tells whoever should hear about a rollback. It is mail and chat and never
// a page: a rollback the factory performed on its own is reported rather than
// requested, and the factory does not page to inform.
func (h *HealthMonitor) report(ctx context.Context, reading Reading) error {
	if h.notifier == nil {
		return nil
	}
	waiting := fmt.Sprintf("release %d was failed by its analysis window and rolled back to release %d, sweeping %d above it",
		reading.Release.Number, reading.Target.Number, len(reading.Rolled.SweptReleaseIDs))
	if reading.WhyNoRollback != "" {
		waiting = fmt.Sprintf("release %d was failed by its analysis window and nothing was rolled back: %s",
			reading.Release.Number, reading.WhyNoRollback)
	}
	_, err := h.notifier.Notify(ctx, notifier.Wait{
		Row:     reading.IncidentID,
		Kind:    notifier.KindRollbackPerformed,
		Waiting: waiting,
	})
	return err
}
