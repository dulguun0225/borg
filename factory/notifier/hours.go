package notifier

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/service"
)

// deferredToHours reports whether a wait of the second kind arrives outside
// its service's authored paging hours, and, where it does, the next instant
// those hours allow it — [Wait.PageAt], computed once here at the deferral. A
// wait is of the second kind where it pages, names a service, is not one of
// [anyHour], and does not carry [Wait.RollbackOutstanding] — production
// serving a release the health monitor called for a rollback on, with the
// rollback not run, being the whole of the first kind. Where an owner
// authors none, or the wait names no service, it is never deferred: pages.md's
// "where an owner authors none, it pages at any hour, which is what every
// service did before there was anything to author."
//
// This decides only whether the page goes out now. Delivering it at the
// instant [Wait.PageAt] names is [Notifier.PageDeferred] below, which its
// caller runs on every pass and which reads the instant back from the
// delivery record rather than recomputing it: an owner narrowing or widening
// the hours after the deferral does not move an hour already announced.
func (n *Notifier) deferredToHours(ctx context.Context, w Wait, now time.Time) (bool, string, error) {
	if w.ServiceID == "" || anyHour[w.Kind] || w.RollbackOutstanding {
		return false, "", nil
	}
	svc, err := service.Get(ctx, n.pool, w.ServiceID)
	if errors.Is(err, service.ErrNotFound) {
		// A wait naming a service this factory holds no record of pages at any
		// hour, the same as one naming none. A missing record is not authored
		// hours, and refusing to deliver on it would let the narrow channel be
		// stopped by a row that is not there.
		return false, "", nil
	} else if err != nil {
		return false, "", fmt.Errorf("notifier: reading %s's paging hours: %w", w.ServiceID, err)
	}
	if !svc.PagingHours.Authored() || withinHours(svc.PagingHours, now) {
		return false, "", nil
	}
	at, err := nextAllowedHour(svc.PagingHours, now)
	if err != nil {
		return false, "", err
	}
	return true, record.FormatTime(at), nil
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

// nextAllowedHour is the next instant, at or after now, that hours.Start
// arrives in hours's zone — the instant [withinHours] starts answering true
// at, and never earlier than now since [deferredToHours] calls this only
// where now already falls outside the hours. A zone this process cannot
// resolve answers now itself, matching [withinHours]'s own conservative
// answer for the same case.
func nextAllowedHour(hours service.PagingHours, now time.Time) (time.Time, error) {
	loc, err := time.LoadLocation(hours.Zone)
	if err != nil {
		return now, nil
	}
	hour, minute, err := parseClock(hours.Start)
	if err != nil {
		return now, err
	}
	local := now.In(loc)
	candidate := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, loc)
	if !candidate.After(local) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	return candidate, nil
}

// parseClock reads an hour authored as HH:MM, the layout [service.SetPagingHours]
// already validated at the write.
func parseClock(clock string) (int, int, error) {
	parts := strings.Split(clock, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("notifier: %q is not an hour in HH:MM", clock)
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("notifier: %q is not an hour in HH:MM: %w", clock, err)
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("notifier: %q is not an hour in HH:MM: %w", clock, err)
	}
	return hour, minute, nil
}

// PageDeferred is what delivers a page a service's paging hours held back: a
// wait of the second kind arising outside them goes out by mail and chat at
// once, waits in Work as every row does, and pages once the instant its own
// delivery record carries in [Wait.PageAt] has passed. Nothing calls the
// notifier again at that hour, so this pass reads its own delivery records
// for the waits whose page channel was skipped and pages each whose instant
// is now behind it.
//
// A candidate is a row this component delivered on mail or chat, naming a
// service, with no delivery on the page channel at all. That is what a page
// held back looks like in the record: the page is the last channel
// [Notifier.Notify] tries and a delivery record is written wherever it is
// attempted, so a row with none was never attempted. A row the log shows closed
// is left alone, a page against a row already resolved being a wait with
// nothing waiting on it, and so is a kind of [anyHour], which the hours never
// held back and whose page channel something else skipped.
//
// driftPool is the drift detector's own store, or nil on an install with no
// detector. It is read for the one wait that ends where nothing calls: a
// mismatch a human cleared there writes into no log of the factory's, so
// without it the hours coming round would page about a mismatch already
// cleared.
//
// It returns the rows it paged. What it costs is a pass of its own: a page
// whose instant falls in the small hours goes out at the first pass after
// that instant rather than at the instant itself.
func (n *Notifier) PageDeferred(ctx context.Context, driftPool *pgxpool.Pool) ([]string, error) {
	held, err := n.deferredDeliveries(ctx)
	if err != nil {
		return nil, err
	}
	if len(held) == 0 {
		return nil, nil
	}
	ended, err := n.endedRows(ctx, driftPool)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	nowAt := record.FormatTime(now)
	var paged []string
	var refused []error
	for _, w := range held {
		if ended[w.Row] || anyHour[w.Kind] {
			// A kind admitted beside the ordinary condition pages at whatever
			// hour its trigger arrived, so one of those that never reached the
			// page channel was held back by something other than the hours —
			// the harm mark's off switch, or its cap, which pages once per
			// interval on a row of its own instead.
			continue
		}
		pages, err := w.pages()
		if err != nil || !pages {
			continue
		}
		if w.PageAt == "" || nowAt < w.PageAt {
			continue
		}
		reach, err := n.routeTo(ctx, w)
		if err != nil {
			return paged, err
		}
		for _, human := range reach {
			if _, err := n.deliver(ctx, Delivery{
				Channel: ChannelPage, To: human, Wait: w, Event: EventReached,
			}); err != nil {
				refused = append(refused, err)
			}
		}
		paged = append(paged, w.Row)
	}
	return paged, errors.Join(refused...)
}

// deferredDeliveries is one wait per row this component delivered on mail or
// chat, about a service, and never on the page channel — rebuilt from the
// delivery record, which is where the wait's own fields, [Wait.PageAt]
// included, are kept for exactly this.
func (n *Notifier) deferredDeliveries(ctx context.Context) ([]Wait, error) {
	stored, err := allDeliveryRows(ctx, n.pool)
	if err != nil {
		return nil, err
	}
	var held []Wait
	for _, r := range stored {
		if r.ServiceID == "" || r.hasChannel(ChannelPage) {
			continue
		}
		w, err := r.wait()
		if err != nil {
			return nil, err
		}
		held = append(held, w)
	}
	return held, nil
}

// endedRows is every row that stopped waiting: what the log shows closed or
// abandoned, and — where a drift detector store is composed — every mismatch a
// human cleared inside it, which is the one wait whose end is written into a
// store no factory component may write and reported to nobody.
func (n *Notifier) endedRows(ctx context.Context, driftPool *pgxpool.Pool) (map[string]bool, error) {
	rows, err := n.reader.Read(ctx, componentPrincipal)
	if err != nil {
		return nil, err
	}
	ended := map[string]bool{}
	for _, row := range rows {
		switch row.Part {
		case decisionlog.PartClose, decisionlog.PartAbandonment:
			ended[row.Closes] = true
		}
	}
	if driftPool == nil {
		return ended, nil
	}
	all, err := driftdetector.All(ctx, driftPool)
	if err != nil {
		return nil, err
	}
	for _, m := range all {
		if m.Cleared() {
			ended[m.ID] = true
		}
	}
	return ended, nil
}
