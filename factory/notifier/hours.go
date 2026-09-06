package notifier

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dulguun0225/borg/factory/service"
)

// deferredToHours reports whether a wait of the second kind arrives outside
// its service's authored paging hours. A wait is of the second kind where it
// pages, names a service, is not one of [anyHour], and does not carry
// [Wait.RollbackOutstanding] — production serving a release the health
// monitor called for a rollback on, with the rollback not run, being the
// whole of the first kind. Where an owner authors none, or the wait names no
// service, it is never deferred: pages.md's "where an owner authors none, it
// pages at any hour, which is what every service did before there was
// anything to author."
//
// What it costs, stated in doc.go: this only decides whether tonight's page
// goes out now. Delivering it at "the next hour the service allows" needs a
// caller to invoke [Notifier.Notify] again once those hours arrive — a
// retry this milestone has no scheduler for — so a deferred page's page
// channel is skipped here and reached only by a later call naming the same
// wait, which nothing yet makes on its own.
func (n *Notifier) deferredToHours(ctx context.Context, w Wait, now time.Time) (bool, error) {
	if w.ServiceID == "" || anyHour[w.Kind] || w.RollbackOutstanding {
		return false, nil
	}
	svc, err := service.Get(ctx, n.pool, w.ServiceID)
	if errors.Is(err, service.ErrNotFound) {
		// A wait naming a service this factory holds no record of pages at any
		// hour, the same as one naming none. A missing record is not authored
		// hours, and refusing to deliver on it would let the narrow channel be
		// stopped by a row that is not there.
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("notifier: reading %s's paging hours: %w", w.ServiceID, err)
	}
	if !svc.PagingHours.Authored() {
		return false, nil
	}
	return !withinHours(svc.PagingHours, now), nil
}

// withinHours reports whether now, read in hours's zone, falls between
// Start and End. A range whose end is not after its start wraps past
// midnight, the ordinary meaning of "22:00 to 06:00".
func withinHours(hours service.PagingHours, now time.Time) bool {
	loc, err := time.LoadLocation(hours.Zone)
	if err != nil {
		// A zone this process cannot resolve is not this function's error to
		// report — Notify already read the record successfully — so the
		// conservative answer is that the hours cover now rather than
		// silently deferring every page a bad zone would otherwise cause.
		return true
	}
	clock := now.In(loc).Format("15:04")
	if hours.Start <= hours.End {
		return clock >= hours.Start && clock < hours.End
	}
	return clock >= hours.Start || clock < hours.End
}
