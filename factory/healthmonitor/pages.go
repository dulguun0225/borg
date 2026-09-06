package healthmonitor

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/window"
)

// theOperationsDuty is duty (2) of the owner's twelve, which is who an open
// incident with no open window reaches: the design names the duty rather than a
// human, and the routing is the notifier's read of the People declaration.
var theOperationsDuty = people.OfDuty(2)

// PageOpenIncidents fires the page an open incident meets the condition for: its
// crossing has not stopped and no window of that service is open. The deployed
// software is worse until a human ends it, whatever the factory has since raised
// from it — raising the item does not answer the page.
//
// It is a pass rather than an event because neither half is an event: a crossing
// that has not stopped is a reading taken again, and a window closing is what
// removes the other half. So the page events already on the incident are read
// before another is written: the first pass that finds the condition pages, the
// next widens it once to the owner where nobody has acknowledged it, and every
// pass after that writes nothing. A page is the sequence of events on one row,
// and a pass that notified each time would reach everybody holding (2) every few
// seconds for as long as the crossing lasted.
//
// It returns the incidents this pass wrote a page event about.
func (h *HealthMonitor) PageOpenIncidents(ctx context.Context, w Watching) ([]string, error) {
	if err := w.validate(); err != nil {
		return nil, err
	}
	if h.pager == nil {
		return nil, nil
	}
	open, err := window.CountOpen(ctx, h.pool, w.ID)
	if err != nil {
		return nil, err
	}
	if open > 0 {
		// A window is open, so the release under watch is the window's own
		// business and the rollback that would end this is still the factory's to
		// perform.
		return nil, nil
	}
	all, err := incident.ForService(ctx, h.pool, w.ID)
	if err != nil {
		return nil, err
	}

	var paged []string
	for _, i := range all {
		if !i.Open() {
			continue
		}
		stopped, err := h.crossingStopped(ctx, w, i)
		if err != nil {
			return paged, err
		}
		if stopped {
			continue
		}
		outstanding, err := h.rollbackOutstanding(ctx, w, i.ReleaseID)
		if err != nil {
			return paged, err
		}
		wait := notifier.Wait{
			Row:  i.ID,
			Kind: notifier.KindIncidentNoOpenWindow,
			Waiting: fmt.Sprintf("%s is still crossing against the release incident %s was raised on, and no window is open",
				w.Name, i.ID),
			Holding: theOperationsDuty, Worse: true, ServiceID: w.ID,
			RollbackOutstanding: outstanding,
		}
		events, err := h.pager.EventsFor(ctx, i.ID)
		if err != nil {
			return paged, err
		}
		var reached, widened, acknowledged bool
		for _, e := range events {
			switch notifier.Event(e.Event) {
			case notifier.EventReached:
				reached = true
			case notifier.EventWidened:
				widened = true
			case notifier.EventAcknowledged:
				acknowledged = true
			}
		}
		switch {
		case !reached:
			if _, err := h.pager.Notify(ctx, wait); err != nil {
				return paged, err
			}
		case !widened && !acknowledged:
			if _, err := h.pager.Widen(ctx, wait); err != nil {
				return paged, err
			}
		default:
			// The page stands and the row still waits; there is no second
			// widening and nothing further to write.
			continue
		}
		paged = append(paged, i.ID)
	}
	return paged, nil
}

// rollbackOutstanding is which of the two kinds a page about this release is:
// production serving a release the health monitor called for a rollback on, with
// the rollback not run, is the first kind and pages at whatever hour the
// condition arose. It is answered from the two records the design names — the
// window that failed the release, and whether a rollback naming it as failed has
// completed — and never from the kind of the wait.
func (h *HealthMonitor) rollbackOutstanding(ctx context.Context, w Watching, releaseID string) (bool, error) {
	if releaseID == "" {
		return false, nil
	}
	win, watched, err := window.ForRelease(ctx, h.pool, releaseID)
	if err != nil {
		return false, err
	}
	if !watched || win.Exit != window.ExitFailed {
		// Nothing called for a rollback on this release, so nothing is waiting to
		// remove it.
		return false, nil
	}
	rollback, found, err := deploy.NewestRollback(ctx, h.pool, w.ID, w.EnvironmentID)
	if err != nil {
		return false, err
	}
	if found && rollback.Undoing.FailedReleaseID == releaseID && rollback.Status == deploy.StatusComplete {
		return false, nil
	}
	return true, nil
}
