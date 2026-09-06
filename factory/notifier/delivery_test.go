// delivery_test.go is the three channels and what a delivery records: a
// refused send on one channel not stopping the next, one delivery record per
// holder, which of the two kinds a wait is, and the harm mark's cap. It is one
// external test package with db_test.go and driftpass_test.go, split by
// subject so each file stays under the line bound.
package notifier_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/service"
)

// refusingOne is a [notifier.Deliverer] that refuses one channel and accepts
// the rest, which is a chat integration that stopped.
type refusingOne struct {
	channel   notifier.Channel
	refuse    error
	delivered []notifier.Delivery
}

func (r *refusingOne) Deliver(_ context.Context, d notifier.Delivery) error {
	if d.Channel == r.channel {
		return r.refuse
	}
	r.delivered = append(r.delivered, d)
	return nil
}

func (r *refusingOne) on(channel notifier.Channel) int {
	n := 0
	for _, d := range r.delivered {
		if d.Channel == channel {
			n++
		}
	}
	return n
}

// TestARefusedChatDoesNotStopThePage is the narrow channel surviving a broken
// one: the page carries what mail and chat cannot, so a chat integration that
// stopped may not suppress it.
func TestARefusedChatDoesNotStopThePage(t *testing.T) {
	ctx, pool, token, _, _ := newNotifier(t)

	refused := errors.New("the chat integration is unreachable")
	channels := &refusingOne{channel: notifier.ChannelChat, refuse: refused}
	n, err := notifier.New(pool, decisionlog.NewWriter(pool, token), token, channels, theOwner)
	if err != nil {
		t.Fatalf("composing the notifier: %v", err)
	}

	waiting := notifier.Wait{
		Row: "mis_chat_down", Kind: notifier.KindDriftMismatch,
		Waiting: "a record disagrees with what runs", Worse: true,
	}
	events, err := n.Notify(ctx, waiting)
	if !errors.Is(err, refused) {
		t.Errorf("Notify = %v, want the chat channel's own refusal carried back", err)
	}
	if channels.on(notifier.ChannelPage) != 1 {
		t.Errorf("the page went out %d time(s) behind a refused chat send, want once",
			channels.on(notifier.ChannelPage))
	}
	if len(events) != 1 {
		t.Errorf("the page wrote %d event(s) behind a refused chat send, want one", len(events))
	}
	_, accepted, found := deliveryRow(t, ctx, pool, waiting.Row, notifier.ChannelChat)
	if !found || accepted {
		t.Errorf("the chat delivery record = accepted %t found %t, want false true", accepted, found)
	}
}

// TestADeliveryRecordIsWrittenPerHolder is what tells "a row no delivery was
// ever accepted for" from "one of two holders was reached": the recipient is
// part of the record's key.
func TestADeliveryRecordIsWrittenPerHolder(t *testing.T) {
	ctx, pool, token, n, _ := newNotifier(t)

	holding := people.OfDuty(12)
	writer := peopleWriter(pool, token)
	for _, key := range []string{"hk_ada", "hk_grace"} {
		if _, err := writer.Declare(ctx, theHumanOwner, key, holding); err != nil {
			t.Fatalf("declaring that %s holds %s: %v", key, holding, err)
		}
	}

	waiting := notifier.Wait{
		Row: "it_two_holders", Kind: notifier.KindItemEscalated,
		Waiting: "the factory gave up on a defect that is live",
		Holding: holding, Worse: true,
	}
	if _, err := n.Notify(ctx, waiting); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	for _, channel := range notifier.Channels {
		var recipients []string
		rows, err := pool.Query(ctx, `select recipient_key from `+notifier.DeliveryTable+`
			where row_id = $1 and channel = $2 order by recipient_key`, waiting.Row, string(channel))
		if err != nil {
			t.Fatalf("reading the delivery records on %s: %v", channel, err)
		}
		for rows.Next() {
			var key string
			if err := rows.Scan(&key); err != nil {
				t.Fatalf("reading a delivery record: %v", err)
			}
			recipients = append(recipients, key)
		}
		rows.Close()
		if len(recipients) != 2 || recipients[0] != "hk_ada" || recipients[1] != "hk_grace" {
			t.Errorf("the delivery records on %s name %v, want one per holder", channel, recipients)
		}
	}
}

// TestAWaitOfTheSecondKindWaitsForTheServicesHours is the split the design
// states: production serving a release the health monitor called for a rollback
// on, with the rollback not run, pages at any hour, and everything else about a
// service waits for the hours that service allows.
func TestAWaitOfTheSecondKindWaitsForTheServicesHours(t *testing.T) {
	ctx, pool, token, n, channels := newNotifier(t)
	serviceID := aServiceWithNoPagingHoursNow(t, ctx, pool, token)

	second := notifier.Wait{
		Row: "mis_second_kind", Kind: notifier.KindDriftMismatch,
		Waiting: "a record disagrees with what runs", Worse: true, ServiceID: serviceID,
	}
	events, err := n.Notify(ctx, second)
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(events) != 0 || channels.on(notifier.ChannelPage) != 0 {
		t.Errorf("a wait of the second kind paged outside the service's hours: %d event(s)", len(events))
	}
	if channels.on(notifier.ChannelMail) != 1 || channels.on(notifier.ChannelChat) != 1 {
		t.Errorf("a deferred wait reached mail %d time(s) and chat %d, want once each",
			channels.on(notifier.ChannelMail), channels.on(notifier.ChannelChat))
	}

	first := second
	first.Row, first.RollbackOutstanding = "win_first_kind", true
	first.Kind, first.Waiting = notifier.KindFailedWithNoRollback, "release 4 failed and no rollback was performed"
	events, err = n.Notify(ctx, first)
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("a wait of the first kind wrote %d page event(s) outside the service's hours, want one", len(events))
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
	overTheCap, err := n.EventsFor(ctx, "harm_mark_page_cap:"+serviceID)
	if err != nil {
		t.Fatalf("EventsFor the cap's own row: %v", err)
	}
	if len(overTheCap) != 1 {
		t.Fatalf("the cap's own row holds %d event(s), want the one page per interval", len(overTheCap))
	}

	// A second intent past the cap inside the same interval adds no second page.
	if _, err := n.Notify(ctx, markedWait("int_five", serviceID)); err != nil {
		t.Fatalf("Notify of a second intent past the cap: %v", err)
	}
	again, err := n.EventsFor(ctx, "harm_mark_page_cap:"+serviceID)
	if err != nil {
		t.Fatalf("EventsFor the cap's own row: %v", err)
	}
	if len(again) != 1 {
		t.Errorf("the cap's own row holds %d event(s) after a second intent past it, want one per interval", len(again))
	}
}

// markedWait is one report marked as describing harm to a person, waiting on
// whoever holds (2).
func markedWait(row, serviceID string) notifier.Wait {
	return notifier.Wait{
		Row: row, Kind: notifier.KindHarmMarkedReport, ServiceID: serviceID, Worse: true,
		Waiting: "a report marked as describing harm to a person",
	}
}

// aServiceWithNoPagingHoursNow is a service whose authored paging hours do not
// cover the moment the test runs, so a wait of the second kind on it is
// deferred and a wait of the first kind is not.
func aServiceWithNoPagingHoursNow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, token lease.Token) string {
	t.Helper()
	writer := service.NewWriter(pool, token)
	svc, err := writer.Create(ctx, record.Actor{Kind: record.KindComponent, Key: "decomposition", Basis: record.BasisClaimed},
		"paged-service", "/srv/repository", "prj_one")
	if err != nil {
		t.Fatalf("creating the service: %v", err)
	}

	// One minute wide, an hour behind now in UTC, so nothing this test does
	// falls inside it.
	past := time.Now().UTC().Add(-time.Hour)
	hours := service.PagingHours{
		Start: past.Format("15:04"), End: past.Add(time.Minute).Format("15:04"), Zone: "UTC",
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, token); err != nil {
		t.Fatalf("fencing: %v", err)
	}
	if err := service.SetPagingHours(ctx, tx, svc.ID, hours); err != nil {
		t.Fatalf("authoring the paging hours: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("committing: %v", err)
	}
	return svc.ID
}
