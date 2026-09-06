package notifier

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/driftdetector"
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
// This decides only whether the page goes out now. Delivering it at the next
// hour the service allows is [Notifier.PageDeferred] below, which its caller
// runs on every pass.
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

// PageDeferred is what delivers a page a service's paging hours held back: a
// wait of the second kind arising outside them goes out by mail and chat at
// once, waits in Work as every row does, and pages at the next hour the
// service allows in that zone. Nothing calls the notifier again at that hour,
// so this pass reads its own delivery records for the waits whose page channel
// was skipped and pages each whose hours now allow it.
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
// It returns the rows it paged. What it costs is a pass of its own: a page held
// to the morning goes out at the first pass after the hours open rather than at
// the hour itself.
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
		deferred, err := n.deferredToHours(ctx, w, now)
		if err != nil {
			return paged, err
		}
		if deferred {
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
// delivery record, which is where the wait's own fields are kept for exactly
// this.
func (n *Notifier) deferredDeliveries(ctx context.Context) ([]Wait, error) {
	rows, err := n.pool.Query(ctx, `select distinct on (d.row_id)
		d.row_id, d.wait_kind, d.service_id, d.waiting, d.holding, d.worse
		from `+DeliveryTable+` d
		where d.channel <> $1 and d.service_id <> ''
		  and not exists (select 1 from `+DeliveryTable+` p
			where p.row_id = d.row_id and p.channel = $1)
		order by d.row_id, d.at`, string(ChannelPage))
	if err != nil {
		return nil, fmt.Errorf("notifier: reading the pages a service's hours held back: %w", err)
	}
	defer rows.Close()

	var held []Wait
	for rows.Next() {
		var w Wait
		var kind, holding string
		if err := rows.Scan(&w.Row, &kind, &w.ServiceID, &w.Waiting, &holding, &w.Worse); err != nil {
			return nil, fmt.Errorf("notifier: reading a page held back: %w", err)
		}
		w.Kind = Kind(kind)
		if holding != "" {
			if w.Holding, err = holdingFrom(holding); err != nil {
				return nil, err
			}
		}
		held = append(held, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("notifier: reading the pages a service's hours held back: %w", err)
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
