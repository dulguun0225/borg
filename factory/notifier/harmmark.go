package notifier

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/record"
)

// harmMarkPagesOff reads whether the harm mark's page is turned off, the
// factory-wide settings' own field: "an owner who will not be woken by a
// stranger turns it off." A factory with no settings record yet — an
// install this milestone does not build — has never turned it off, so the
// zero value's "not found" reads as on, matching the shipped default the
// factorysettings package itself carries.
func harmMarkPagesOff(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	settings, err := factorysettings.Get(ctx, pool)
	if errors.Is(err, factorysettings.ErrNotFound) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("notifier: reading whether the harm mark's page is on: %w", err)
	}
	return !settings.HarmMarkPages, nil
}

// capRowPrefix names every row [capRow] mints, ahead of the service and the
// interval it names. [Notifier.stillWaitingBySubject] reads it to tell the
// cap's own row apart from an ordinary marked report's, the two sharing
// [KindHarmMarkedReport] and nothing else naming which is which.
const capRowPrefix = "harm_mark_page_cap:"

// capRow is the row one interval's page is delivered about, per service: the
// cap's own page is not about any one intent, so it takes a row of its own
// rather than the row of whichever marked intent happened to arrive past the
// cap. bucket names which interval, so the interval after the one a row
// already answered for gets a row of its own and pages again — nothing ever
// answers this row itself, there being no closing act for a page about a
// volume rather than about one thing.
func capRow(serviceID string, bucket int64) string {
	return fmt.Sprintf("%s%s:%d", capRowPrefix, serviceID, bucket)
}

// intervalBucket is which fixed-width interval of intervalSeconds, counted
// from the Unix epoch, now falls in — what makes two calls inside one
// interval agree on the row [capRow] names and a call in the interval after
// mint a row of its own.
func intervalBucket(now time.Time, intervalSeconds int64) int64 {
	return now.Unix() / intervalSeconds
}

// overHarmMarkCap is the harm mark's cap read for one wait: how many intents of
// this service's marked reports have already been paged inside the interval in
// force, against the cap in force. Past it the marked intent waits at Work as
// every marked one does and its page channel is skipped, and one page per
// interval goes out instead naming the service and how many arrived past the
// cap — which is what [Notifier.pageOverTheCap] delivers.
//
// A wait naming no service is never over the cap: the cap is per service, and
// the value in force is read against one.
//
// It returns, beside whether the wait is over the cap and how many arrived
// past it, the interval in force in seconds: what [Notifier.pageOverTheCap]
// and [Notifier.stillWaitingBySubject] need to name the interval a cap row is
// for, without reading the settings record a second time.
func (n *Notifier) overHarmMarkCap(ctx context.Context, w Wait, now time.Time) (bool, int, int64, error) {
	if w.ServiceID == "" {
		return false, 0, 0, nil
	}
	// The shipped cap holds whether or not a settings record exists: an install
	// with none has authored nothing, which is the default and not the absence
	// of one.
	inForce := factorysettings.PageCap{
		Cap: factorysettings.DefaultHarmMarkPageCap, IntervalSeconds: factorysettings.DefaultHarmMarkPageInterval,
	}
	settings, err := factorysettings.Get(ctx, n.pool)
	if err != nil && !errors.Is(err, factorysettings.ErrNotFound) {
		return false, 0, 0, fmt.Errorf("notifier: reading the harm mark's cap: %w", err)
	}
	if err == nil {
		if inForce, err = factorysettings.HarmMarkPageCap(ctx, n.pool, settings.ID, w.ServiceID); err != nil {
			return false, 0, 0, err
		}
	}
	since := record.FormatTime(now.Add(-time.Duration(inForce.IntervalSeconds) * time.Second))
	paged, err := PagedRowsSince(ctx, n.pool, w.ServiceID, KindHarmMarkedReport, since)
	if err != nil {
		return false, 0, 0, err
	}
	if paged < inForce.Cap {
		return false, 0, inForce.IntervalSeconds, nil
	}
	// Past the cap, the count this page reports is every marked intent beyond
	// it, this one included: paged holds the ones that went out, and the excess
	// is what waited plus the one being decided now.
	return true, paged - inForce.Cap + 1, inForce.IntervalSeconds, nil
}

// pageOverTheCap is the one page per interval that goes out instead of the
// marked intent's own: it names the service and how many marked intents
// arrived past the cap, so the human reached reads that volume is what they
// are looking at. intervalSeconds and now are what [capRow] names its own
// row's interval from, so a call in the interval after the one a row already
// answered for mints a row of its own: nothing ever answers this row, there
// being no closing act for a page about a volume rather than about one thing,
// and a row with a reached event already on it is this interval's own page
// already sent.
func (n *Notifier) pageOverTheCap(ctx context.Context, w Wait, past int, intervalSeconds int64, now time.Time) error {
	row := capRow(w.ServiceID, intervalBucket(now, intervalSeconds))
	events, err := n.EventsFor(ctx, row)
	if err != nil {
		return err
	}
	for _, e := range events {
		if Event(e.Event) == EventReached {
			return nil
		}
	}
	over := Wait{
		Row: row, Kind: KindHarmMarkedReport, Holding: w.Holding, Worse: true, ServiceID: w.ServiceID,
		Waiting: fmt.Sprintf("%d marked intent(s) on this service arrived past the cap on how many may page per interval",
			past),
	}
	reach, err := n.routeTo(ctx, over)
	if err != nil {
		return err
	}
	for _, human := range reach {
		if _, err := n.deliver(ctx, Delivery{
			Channel: ChannelPage, To: human, Wait: over, Event: EventReached,
		}); err != nil {
			return err
		}
	}
	return nil
}
