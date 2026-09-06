package healthmonitor

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/window"
)

// TargetBelow is a rollback's target: the newest release of the service below
// number whose window closed passed or timed out, and false where there is none.
// It answers two questions with one query, and they are the same question asked
// at two moments:
//
//   - what a rollback of that release returns to, which the design computes for
//     one rollback and never per service, because a query stated per service
//     alone would return a release above the failed one and the factory would
//     restore the change it had just failed;
//   - which build the control started beside that release runs, which needs the
//     same property for the same reason — a control on the ordinal predecessor
//     would restart failed code onto production traffic for the whole of the
//     next window.
//
// It descends past failed, past skipped, past any window still open, past a
// window that measured nothing, and past a
// release whose deploy stopped before its build took traffic: that release never
// ran and its change never landed, so returning to it would redeploy a build
// over a store missing the change it shipped.
//
// Nothing writes it: the release record is written once at the fast-forward and
// never again, so an outcome settled by a window closing long afterwards cannot
// be a field of it. What that costs is that every path computes it, and that a
// window which fails to close leaves the answer older than it should be — the
// rollback goes further back and undoes releases nothing failed, up to the
// window limit, which is the safe direction and still a real loss.
func (h *HealthMonitor) TargetBelow(ctx context.Context, w Watching, number int64) (release.Release, bool, error) {
	closed, err := window.ClosedPassedOrTimedOut(ctx, h.pool, w.ID)
	if err != nil {
		return release.Release{}, false, err
	}

	var best release.Release
	found := false
	for _, win := range closed {
		if win.ReleaseID == "" {
			// A search's window names a build and no release, and no rollback returns
			// to a build that is on no branch.
			continue
		}
		r, err := release.Get(ctx, h.pool, win.ReleaseID)
		if err != nil {
			return release.Release{}, false, fmt.Errorf("healthmonitor: reading the release window %s watched: %w", win.ID, err)
		}
		if r.Number >= number || (found && r.Number <= best.Number) {
			continue
		}
		tookTraffic, err := h.tookTraffic(ctx, w, r.ID)
		if err != nil {
			return release.Release{}, false, err
		}
		if !tookTraffic {
			continue
		}
		best, found = r, true
	}
	return best, found, nil
}

// LastKnownGood is the service's standing value: the release watched by the
// newest closed window that measured something and whose exit is passed or timed
// out, descending past a
// release whose deploy stopped before its build took traffic. It is what a
// rollback could return to at all and where declarations in force start.
//
// It differs from [HealthMonitor.TargetBelow] exactly where windows close out of
// the order they opened in: stated per service alone, it can name a release
// above the one being rolled back.
func (h *HealthMonitor) LastKnownGood(ctx context.Context, w Watching) (release.Release, bool, error) {
	closed, err := window.ClosedPassedOrTimedOut(ctx, h.pool, w.ID)
	if err != nil {
		return release.Release{}, false, err
	}
	for _, win := range closed {
		if win.ReleaseID == "" {
			continue
		}
		r, err := release.Get(ctx, h.pool, win.ReleaseID)
		if err != nil {
			return release.Release{}, false, fmt.Errorf("healthmonitor: reading the release window %s watched: %w", win.ID, err)
		}
		tookTraffic, err := h.tookTraffic(ctx, w, r.ID)
		if err != nil {
			return release.Release{}, false, err
		}
		if tookTraffic {
			return r, true, nil
		}
	}
	return release.Release{}, false, nil
}

// tookTraffic is whether any deploy of that release into this environment got
// far enough for the build to serve. A release whose deploy stopped before its
// build took traffic — a schema change that failed to apply — counts for neither
// query, however its window closed.
//
// Completion is per target and not per record, so this reads every target of
// every deploy of the release: one target ever marked complete or rolled back
// is one the build served on. A deploy that never reached any target is one no
// target ever took. What it costs while a release mid-rollout has some targets
// still not_reached is that it still reads as having taken traffic the moment
// the first one completes, which is the safe direction — the one the design
// already names for a window that fails to close.
func (h *HealthMonitor) tookTraffic(ctx context.Context, w Watching, releaseID string) (bool, error) {
	deploys, err := deploy.ByRelease(ctx, h.pool, w.EnvironmentID, releaseID)
	if err != nil {
		return false, err
	}
	for _, d := range deploys {
		targets, err := deploy.Targets(ctx, h.pool, d.ID)
		if err != nil {
			return false, err
		}
		for _, t := range targets {
			if t.Completion == deploy.CompletionComplete || t.Completion == deploy.CompletionRolledBack {
				return true, nil
			}
		}
	}
	return false, nil
}
