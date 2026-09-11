// delivery_test.go is the three channels and what a delivery records: a
// refused send on one channel not stopping the next, one delivery record per
// holder, and which of the two kinds a wait is. It is one external test
// package with db_test.go, pageevents_test.go, driftpass_test.go,
// deliveryread_test.go and harmmark_test.go, split by subject so each file
// stays under the line bound — deliveryread_test.go holds
// [notifier.DeliveryRecord.FirstAcceptedAt] and [notifier.DeliveriesOf], and
// harmmark_test.go holds the harm mark's off switch and its cap.
package notifier_test

import (
	"context"
	"errors"
	"sort"
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
// ever accepted for" from "one of two holders was reached": each holder's own
// attempt on each channel is kept, even though the record itself is one per
// waiting row and not one per channel and recipient.
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
	records, err := notifier.DeliveriesOf(ctx, pool, waiting.Row)
	if err != nil {
		t.Fatalf("DeliveriesOf: %v", err)
	}
	for _, channel := range notifier.Channels {
		var recipients []string
		for _, r := range records {
			if r.Channel == channel {
				recipients = append(recipients, r.RecipientKey)
			}
		}
		sort.Strings(recipients)
		if len(recipients) != 2 || recipients[0] != "hk_ada" || recipients[1] != "hk_grace" {
			t.Errorf("the delivery records on %s name %v, want one per holder", channel, recipients)
		}
	}
}

// TestTheDeliveryIsOneRecordPerWaitingRow is finding 2's own fix: however
// many channels and recipients a row was attempted on, the store keeps one
// row for it in [notifier.DeliveryRowTable] and not one per attempt.
func TestTheDeliveryIsOneRecordPerWaitingRow(t *testing.T) {
	ctx, pool, token, n, _ := newNotifier(t)

	holding := people.OfDuty(12)
	writer := peopleWriter(pool, token)
	for _, key := range []string{"hk_ada", "hk_grace"} {
		if _, err := writer.Declare(ctx, theHumanOwner, key, holding); err != nil {
			t.Fatalf("declaring that %s holds %s: %v", key, holding, err)
		}
	}

	waiting := notifier.Wait{
		Row: "it_one_row_per_waiting_row", Kind: notifier.KindItemEscalated,
		Waiting: "the factory gave up on a defect that is live",
		Holding: holding, Worse: true,
	}
	if _, err := n.Notify(ctx, waiting); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	var rowCount int
	if err := pool.QueryRow(ctx, `select count(*) from `+notifier.DeliveryRowTable+` where row_id = $1`,
		waiting.Row).Scan(&rowCount); err != nil {
		t.Fatalf("counting the rows of %s: %v", notifier.DeliveryRowTable, err)
	}
	if rowCount != 1 {
		t.Errorf("%s holds %d row(s) for %s, want one however many channels and holders it reached",
			notifier.DeliveryRowTable, rowCount, waiting.Row)
	}

	records, err := notifier.DeliveriesOf(ctx, pool, waiting.Row)
	if err != nil {
		t.Fatalf("DeliveriesOf: %v", err)
	}
	if len(records) != len(notifier.Channels)*2 {
		t.Errorf("DeliveriesOf returned %d record(s), want %d: one per channel and holder",
			len(records), len(notifier.Channels)*2)
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

// TestAPageHeldToTheHoursGoesOutWhenTheyComeRound is the rest of that split: a
// wait of the second kind arising outside a service's hours goes out by mail
// and chat at once and pages once the instant [notifier.Wait.PageAt] names has
// passed, which is what [notifier.Notifier.PageDeferred] delivers rather than
// the page being dropped. The instant is frozen at the deferral, so this test
// moves it into the past directly on the delivery record rather than by
// widening the service's hours, which the deferral has already read.
func TestAPageHeldToTheHoursGoesOutWhenTheyComeRound(t *testing.T) {
	ctx, pool, token, n, channels := newNotifier(t)
	serviceID := aServiceWithNoPagingHoursNow(t, ctx, pool, token)

	held := notifier.Wait{
		Row: "mis_held", Kind: notifier.KindDriftMismatch,
		Waiting: "a record disagrees with what runs", Worse: true, ServiceID: serviceID,
		Holding: people.OfObligation(people.ObligationDriftDetector),
	}
	if _, err := n.Notify(ctx, held); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if channels.on(notifier.ChannelPage) != 0 {
		t.Fatalf("the wait paged outside the service's hours")
	}

	// A pass while the computed instant is still ahead delivers nothing.
	paged, err := n.PageDeferred(ctx, nil)
	if err != nil {
		t.Fatalf("PageDeferred outside the hours: %v", err)
	}
	if len(paged) != 0 || channels.on(notifier.ChannelPage) != 0 {
		t.Fatalf("PageDeferred paged %v while the service's hours are closed", paged)
	}

	pageAtHasPassed(t, ctx, pool, held.Row)

	paged, err = n.PageDeferred(ctx, nil)
	if err != nil {
		t.Fatalf("PageDeferred: %v", err)
	}
	if len(paged) != 1 || paged[0] != held.Row {
		t.Fatalf("PageDeferred paged %v, want the row held to the hours", paged)
	}
	events, err := n.EventsFor(ctx, held.Row)
	if err != nil {
		t.Fatalf("EventsFor: %v", err)
	}
	if len(events) != 1 || notifier.Event(events[0].Event) != notifier.EventReached {
		t.Fatalf("the row holds %+v, want the one reached event the delayed page wrote", events)
	}
	if channels.on(notifier.ChannelMail) != 1 || channels.on(notifier.ChannelChat) != 1 {
		t.Errorf("the delayed page delivered mail %d time(s) and chat %d, want the once each Notify already made",
			channels.on(notifier.ChannelMail), channels.on(notifier.ChannelChat))
	}

	// A second pass finds the page delivered and adds nothing.
	again, err := n.PageDeferred(ctx, nil)
	if err != nil {
		t.Fatalf("PageDeferred again: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("a second pass paged %v, want nothing left held", again)
	}
}

// pageAtHasPassed moves row's stored [notifier.Wait.PageAt] into the past
// directly, which is what the instant the deferral computed doing so on its
// own looks like from this package's own record: the frozen instant, and not
// the service's hours, is what [notifier.Notifier.PageDeferred] reads.
func pageAtHasPassed(t *testing.T, ctx context.Context, pool *pgxpool.Pool, row string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `update `+notifier.DeliveryRowTable+` set page_at = $1 where row_id = $2`,
		record.FormatTime(time.Now().Add(-time.Hour)), row); err != nil {
		t.Fatalf("moving %s's page_at into the past: %v", row, err)
	}
}

// authorHoursCovering writes paging hours that cover at, which is the next hour
// the service allows arriving.
func authorHoursCovering(t *testing.T, ctx context.Context, pool *pgxpool.Pool,
	token lease.Token, serviceID string, at time.Time) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, token); err != nil {
		t.Fatalf("fencing: %v", err)
	}
	hours := service.PagingHours{
		Start: at.UTC().Add(-time.Hour).Format("15:04"),
		End:   at.UTC().Add(time.Hour).Format("15:04"),
		Zone:  "UTC",
	}
	if err := service.SetPagingHours(ctx, tx, serviceID, hours); err != nil {
		t.Fatalf("authoring the paging hours: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("committing: %v", err)
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
