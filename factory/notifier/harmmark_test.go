// harmmark_test.go is the harm mark's off switch and its cap: the switch
// governing only the page, and the cap's own alert going out again once its
// interval has passed. Split out of delivery_test.go, which
// aServiceWithNoPagingHoursNow and authorHoursCovering stay in, being shared
// with driftpass_test.go and hours_test.go too.
package notifier_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/record"
)

// markedWait is one report marked as describing harm to a person, waiting on
// whoever holds (2).
func markedWait(row, serviceID string) notifier.Wait {
	return notifier.Wait{
		Row: row, Kind: notifier.KindHarmMarkedReport, ServiceID: serviceID, Worse: true,
		Waiting: "a report marked as describing harm to a person",
	}
}

// authorFactorySettings ensures the factory-wide settings record exists and
// applies write inside the same transaction it was created or found in.
func authorFactorySettings(t *testing.T, ctx context.Context, pool *pgxpool.Pool, token lease.Token,
	write func(tx pgx.Tx, settingsID string) error) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	settings, err := factorysettings.Insert(ctx, tx, token, testActor, record.NewID(factorysettings.IDPrefix))
	if err != nil {
		t.Fatalf("inserting the factory-wide settings record: %v", err)
	}
	if err := write(tx, settings.ID); err != nil {
		t.Fatalf("authoring the setting: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("committing: %v", err)
	}
}

// TestHarmMarkPagesOffLeavesMailAndChatDelivered is C2129: the off switch
// governs only whether the page fires. Mail and chat still go out, which is
// what an owner turning off a stranger's page did not ask to lose.
func TestHarmMarkPagesOffLeavesMailAndChatDelivered(t *testing.T) {
	ctx, pool, token, n, channels := newNotifier(t)
	serviceID := aServiceWithNoPagingHoursNow(t, ctx, pool, token)
	authorFactorySettings(t, ctx, pool, token, func(tx pgx.Tx, id string) error {
		return factorysettings.SetHarmMarkPages(ctx, tx, id, false)
	})

	events, err := n.Notify(ctx, markedWait("int_off", serviceID))
	if err != nil {
		t.Fatalf("Notify with the harm mark's page off: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("Notify wrote %d page event(s) with the harm mark's page off, want none", len(events))
	}
	if channels.on(notifier.ChannelPage) != 0 {
		t.Errorf("the page channel was reached %d time(s) with the harm mark's page off, want none",
			channels.on(notifier.ChannelPage))
	}
	if channels.on(notifier.ChannelMail) != 1 || channels.on(notifier.ChannelChat) != 1 {
		t.Errorf("mail reached %d time(s) and chat %d with the harm mark's page off, want the switch to govern only the page",
			channels.on(notifier.ChannelMail), channels.on(notifier.ChannelChat))
	}
}

// TestTheHarmMarkCapPagesOncePerIntervalPastIt is the cap: past it a marked
// intent's own page channel is skipped and one page goes out naming the service
// and how many arrived past it.
func TestTheHarmMarkCapPagesOncePerIntervalPastIt(t *testing.T) {
	ctx, pool, token, n, channels := newNotifier(t)
	serviceID := aServiceWithNoPagingHoursNow(t, ctx, pool, token)

	for i, row := range []string{"int_one", "int_two", "int_three"} {
		if _, err := n.Notify(ctx, markedWait(row, serviceID)); err != nil {
			t.Fatalf("Notify of marked intent %d: %v", i, err)
		}
	}
	if channels.on(notifier.ChannelPage) != 3 {
		t.Fatalf("the first three marked intents paged %d time(s), want the shipped cap of three",
			channels.on(notifier.ChannelPage))
	}

	events, err := n.Notify(ctx, markedWait("int_four", serviceID))
	if err != nil {
		t.Fatalf("Notify past the cap: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("the marked intent past the cap paged on its own row: %d event(s)", len(events))
	}
	if channels.on(notifier.ChannelPage) != 4 {
		t.Fatalf("the page channel was reached %d time(s) past the cap, want the cap's own page added to the first three",
			channels.on(notifier.ChannelPage))
	}

	// A second intent past the cap inside the same interval adds no second page.
	if _, err := n.Notify(ctx, markedWait("int_five", serviceID)); err != nil {
		t.Fatalf("Notify of a second intent past the cap: %v", err)
	}
	if channels.on(notifier.ChannelPage) != 4 {
		t.Errorf("the page channel was reached %d time(s) after a second intent past it, want one per interval",
			channels.on(notifier.ChannelPage))
	}
}

// TestTheHarmMarkCapPagesAgainNextInterval is C2128: the cap's own page is
// not answered by anything, so what makes it fire again in the interval
// after is a row of its own per interval rather than the one the first
// interval used.
func TestTheHarmMarkCapPagesAgainNextInterval(t *testing.T) {
	ctx, pool, token, n, channels := newNotifier(t)
	serviceID := aServiceWithNoPagingHoursNow(t, ctx, pool, token)
	authorFactorySettings(t, ctx, pool, token, func(tx pgx.Tx, id string) error {
		return factorysettings.SetHarmMarkPageCap(ctx, tx, testActor, id, serviceID, 0, 1)
	})

	if _, err := n.Notify(ctx, markedWait("roll_one", serviceID)); err != nil {
		t.Fatalf("Notify past a cap of zero: %v", err)
	}
	if channels.on(notifier.ChannelPage) != 1 {
		t.Fatalf("the first report past a cap of zero paged %d time(s), want one",
			channels.on(notifier.ChannelPage))
	}

	time.Sleep(1100 * time.Millisecond)

	if _, err := n.Notify(ctx, markedWait("roll_two", serviceID)); err != nil {
		t.Fatalf("Notify in the next interval: %v", err)
	}
	if channels.on(notifier.ChannelPage) != 2 {
		t.Errorf("the next interval past the cap paged %d time(s) in total, want a second page for the new interval",
			channels.on(notifier.ChannelPage))
	}
}
