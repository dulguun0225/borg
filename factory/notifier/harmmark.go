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

// capRow is the row the one page per interval is delivered about, per service:
// the cap's own page is not about any one intent, so it takes a row of its own
// rather than the row of whichever marked intent happened to arrive past the
// cap. It repeats per interval the way [driftDetectorStaleRow] repeats per
// episode, the events since the last answered one describing the interval this
// call is deciding about.
func capRow(serviceID string) string { return "harm_mark_page_cap:" + serviceID }

// overHarmMarkCap is the harm mark's cap read for one wait: how many intents of
// this service's marked reports have already been paged inside the interval in
// force, against the cap in force. Past it the marked intent waits at Work as
// every marked one does and its page channel is skipped, and one page per
// interval goes out instead naming the service and how many arrived past the
// cap — which is what [Notifier.pageOverTheCap] delivers.
//
// A wait naming no service is never over the cap: the cap is per service, and
// the value in force is read against one.
func (n *Notifier) overHarmMarkCap(ctx context.Context, w Wait, now time.Time) (bool, int, error) {
	if w.ServiceID == "" {
		return false, 0, nil
	}
	// The shipped cap holds whether or not a settings record exists: an install
	// with none has authored nothing, which is the default and not the absence
	// of one.
	inForce := factorysettings.PageCap{
		Cap: factorysettings.DefaultHarmMarkPageCap, IntervalSeconds: factorysettings.DefaultHarmMarkPageInterval,
	}
	settings, err := factorysettings.Get(ctx, n.pool)
	if err != nil && !errors.Is(err, factorysettings.ErrNotFound) {
		return false, 0, fmt.Errorf("notifier: reading the harm mark's cap: %w", err)
	}
	if err == nil {
		if inForce, err = factorysettings.HarmMarkPageCap(ctx, n.pool, settings.ID, w.ServiceID); err != nil {
			return false, 0, err
		}
	}
	since := record.FormatTime(now.Add(-time.Duration(inForce.IntervalSeconds) * time.Second))
	paged, err := PagedRowsSince(ctx, n.pool, w.ServiceID, KindHarmMarkedReport, since)
	if err != nil {
		return false, 0, err
	}
	if paged < inForce.Cap {
		return false, 0, nil
	}
	// Past the cap, the count this page reports is every marked intent beyond
	// it, this one included: paged holds the ones that went out, and the excess
	// is what waited plus the one being decided now.
	return true, paged - inForce.Cap + 1, nil
}

// pageOverTheCap is the one page per interval that goes out instead of the
// marked intent's own: it names the service and how many marked intents
// arrived past the cap, so the human reached reads that volume is what they
// are looking at. It is delivered once per interval, which is the row's own
// events since the last answered one saying nothing has been delivered yet.
func (n *Notifier) pageOverTheCap(ctx context.Context, w Wait, past int) error {
	row := capRow(w.ServiceID)
	events, err := n.EventsFor(ctx, row)
	if err != nil {
		return err
	}
	for i, e := range events {
		if Event(e.Event) == EventAnswered {
			events = events[i+1:]
		}
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
