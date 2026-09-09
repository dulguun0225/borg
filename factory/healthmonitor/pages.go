package healthmonitor

import (
	"context"
	"fmt"
	"slices"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/lastcheck"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/service"
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

// PageRollbackNotComplete fires the fifth page condition: a rollback this
// component called for whose deploy record is still not complete on every
// target at the deployer's next last check for that environment. Production
// serves a release the factory has already failed, and the mechanism that would
// remove it did not finish — the deployer stopped, or a target accepted the
// shift and never completed it.
//
// The deployer's own last check is what makes the condition readable and what
// bounds it: a rollback still running is not one that stopped, so nothing fires
// until the deployer has recorded a pass over one of the service's targets
// after the rollback's record was written. Without that bound every rollback
// would page in the seconds between its record and its completion.
//
// It is a pass over a standing condition rather than an event, so the page
// events already on the row are read before another is written, the way
// [HealthMonitor.PageOpenIncidents] reads them: the first pass that finds the
// condition pages, the next widens it once to the owner where nobody has
// acknowledged it, and every pass after that writes nothing.
//
// It returns the deploy record this pass wrote a page event about, empty where
// it wrote none.
func (h *HealthMonitor) PageRollbackNotComplete(ctx context.Context, w Watching) (string, error) {
	if err := w.validate(); err != nil {
		return "", err
	}
	if h.pager == nil {
		return "", nil
	}
	rollback, found, err := deploy.NewestRollback(ctx, h.pool, w.ID, w.EnvironmentID)
	if err != nil || !found {
		return "", err
	}
	if rollback.Undoing.Source != deploy.SourceHealthMonitorAtFailed || rollback.Status == deploy.StatusComplete {
		return "", nil
	}
	passed, err := h.deployerPassedSince(ctx, w, rollback.At)
	if err != nil || !passed {
		return "", err
	}

	wait := notifier.Wait{
		Row:  rollback.ID,
		Kind: notifier.KindRollbackIncomplete,
		Waiting: fmt.Sprintf("the rollback of %s to release %s is not complete on every target, and the deployer has made a pass since it was called for",
			w.Name, rollback.ReleaseID),
		Holding: theOperationsDuty, Worse: true, ServiceID: w.ID,
		// Production serves a release this component has already failed and the
		// rollback that would remove it did not finish, which is the design's
		// first kind of wait: the software is worse for every hour of it, so the
		// page fires at whatever hour the condition arose.
		RollbackOutstanding: true,
	}
	events, err := h.pager.EventsFor(ctx, rollback.ID)
	if err != nil {
		return "", err
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
		_, err = h.pager.Notify(ctx, wait)
	case !widened && !acknowledged:
		_, err = h.pager.Widen(ctx, wait)
	default:
		// The page stands and the row still waits; there is no second widening
		// and nothing further to write.
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return rollback.ID, nil
}

// deployerPassedSince is whether the deployer has recorded a last check over
// any target this service runs on since the time given. It is the deployer's
// next last check for the environment, which is what the condition above is
// read at: the deployer keeps one per target of a persistent environment, and a
// pass over any of them is the deployer having run since.
func (h *HealthMonitor) deployerPassedSince(ctx context.Context, w Watching, since string) (bool, error) {
	svc, err := service.Get(ctx, h.pool, w.ID)
	if err != nil {
		return false, err
	}
	addresses := targetsOrDefault(svc.Targets, w)
	checks, err := lastcheck.ForComponent(ctx, h.pool, lastcheck.ComponentDeployer)
	if err != nil {
		return false, err
	}
	for _, check := range checks {
		if slices.Contains(addresses, check.Subject) && check.CheckedAt > since {
			return true, nil
		}
	}
	return false, nil
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
