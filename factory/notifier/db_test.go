// The notifier's own tests: what qualifies for a page, who a delivery reaches, and
// the three events a page is a sequence of. The page events are rows of the decision
// log, so this reaches a database; mail and chat write nothing, which is one of the
// things asserted here.
//
// These tests do not skip when the database is unreachable — the milestone is
// demonstrated by them running, so an unreachable database fails the run.
package notifier_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

// theOwner is who a page widens to. The notifier is composed with the name, because
// the design gives the owner no record.
const theOwner = "owner"

// recorder is a [notifier.Deliverer] that reaches nothing and keeps what it was
// handed, which is what says a delivery happened on a channel that writes no record.
type recorder struct {
	delivered []notifier.Delivery
	refuse    error
}

func (r *recorder) Deliver(_ context.Context, d notifier.Delivery) error {
	if r.refuse != nil {
		return r.refuse
	}
	r.delivered = append(r.delivered, d)
	return nil
}

// on is how many deliveries went out on one channel.
func (r *recorder) on(channel notifier.Channel) int {
	n := 0
	for _, d := range r.delivered {
		if d.Channel == channel {
			n++
		}
	}
	return n
}

// newNotifier gives a test a schema of its own with the whole factory schema
// applied, a recorder in place of the three channels, and the notifier over both.
func newNotifier(t *testing.T) (context.Context, *pgxpool.Pool, *notifier.Notifier, *recorder) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "notifier_" + hex.EncodeToString(suffix[:])

	pool, err := postgres.Open(ctx, inSchema(t, postgres.URL(), schema))
	if err != nil {
		t.Fatalf("the database at %s is not reachable, and these tests do not skip: %v", postgres.URL(), err)
	}
	t.Cleanup(func() {
		drop, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := pool.Exec(drop, `drop schema if exists `+pgx.Identifier{schema}.Sanitize()+` cascade`); err != nil {
			t.Errorf("dropping schema %s: %v", schema, err)
		}
		pool.Close()
	})
	if _, err := pool.Exec(ctx, `create schema `+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("creating schema %s: %v", schema, err)
	}
	if err := postgres.Apply(ctx, pool); err != nil {
		t.Fatalf("applying the schema: %v", err)
	}

	channels := &recorder{}
	n, err := notifier.New(pool, decisionlog.NewWriter(pool), channels, theOwner)
	if err != nil {
		t.Fatalf("composing the notifier: %v", err)
	}
	return ctx, pool, n, channels
}

// inSchema points a connection URL at one schema and nothing else, so every
// unqualified name in the DDL and in the writers' statements resolves there.
func inSchema(t *testing.T, base, schema string) string {
	t.Helper()
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parsing %s: %v", base, err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// TestOnlyAPageWritesARecord is the narrow channel: everything waiting on a human
// goes out on mail and chat, and neither writes anything, because a delivery that
// changes no state is no evidence and a record per delivery would size the log by how
// often the factory notifies.
func TestOnlyAPageWritesARecord(t *testing.T) {
	ctx, pool, n, channels := newNotifier(t)

	waiting := notifier.Wait{
		Row: "dl_apending", Kind: notifier.KindGateDecision,
		Waiting: "a human decides at the merge row",
	}
	events, err := n.Notify(ctx, waiting)
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("a gate decision wrote %d page event(s), and nothing live is worse until a human closes one", len(events))
	}
	if channels.on(notifier.ChannelMail) != 1 || channels.on(notifier.ChannelChat) != 1 {
		t.Errorf("mail went out %d times and chat %d, want one each",
			channels.on(notifier.ChannelMail), channels.on(notifier.ChannelChat))
	}
	if channels.on(notifier.ChannelPage) != 0 {
		t.Error("a page went out about a gate decision")
	}

	rows, err := decisionlog.Read(ctx, pool)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("the log holds %d rows after mail and chat, and neither writes one", len(rows))
	}
}

// TestAPageWritesOneReachedEventPerHolder is the first delivery: it reaches every
// human holding the duty at once, there being no rotation naming which one it reaches
// first.
func TestAPageWritesOneReachedEventPerHolder(t *testing.T) {
	ctx, pool, n, channels := newNotifier(t)

	holding := people.OfDuty(12)
	writer := people.NewWriter(pool)
	owner := record.Actor{Kind: record.KindHuman, Name: theOwner}
	for _, human := range []string{"ada", "grace"} {
		if _, err := writer.Declare(ctx, owner, human, holding); err != nil {
			t.Fatalf("declaring that %s holds %s: %v", human, holding, err)
		}
	}

	waiting := notifier.Wait{
		Row: "it_stuck", Kind: notifier.KindItemEscalated,
		Waiting: "the factory gave up on a defect that is live",
		Holding: holding, Worse: true,
	}
	events, err := n.Notify(ctx, waiting)
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("the page wrote %d event(s), and two humans hold the duty", len(events))
	}
	if channels.on(notifier.ChannelPage) != 2 {
		t.Errorf("the page went out %d times, want one per holder", channels.on(notifier.ChannelPage))
	}

	read, err := notifier.EventsFor(ctx, pool, waiting.Row)
	if err != nil {
		t.Fatalf("EventsFor: %v", err)
	}
	if len(read) != 2 {
		t.Fatalf("the page's sequence is %d event(s) long, want two", len(read))
	}
	reached := map[string]bool{}
	for _, e := range read {
		if notifier.Event(e.Event) != notifier.EventReached {
			t.Errorf("the first deliveries include a %q event", e.Event)
		}
		if e.Kind != notifier.PageEventKind || e.Row != waiting.Row {
			t.Errorf("the event says kind %q about %q", e.Kind, e.Row)
		}
		if e.Holding != holding.String() {
			t.Errorf("the event routed by %q, want %q", e.Holding, holding)
		}
		reached[e.Reached] = true
	}
	if !reached["ada"] || !reached["grace"] {
		t.Errorf("the page reached %v, want both holders", reached)
	}

	// The rows are the log's, written as the notifier and not as whoever created the
	// wait: who created it is on that wait's own record.
	rows, err := decisionlog.Read(ctx, pool)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	for _, row := range rows {
		if row.Shape != decisionlog.ShapePageEvent {
			t.Errorf("row %s is shape %s, and a page event is its own shape", row.ID, row.Shape)
		}
		if row.Actor != notifier.Actor {
			t.Errorf("row %s was written as %+v, want the notifier", row.ID, row.Actor)
		}
	}
	if err := decisionlog.Verify(ctx, pool); err != nil {
		t.Errorf("the chain does not verify over page events: %v", err)
	}
}

// TestADutyNobodyHoldsReachesTheOwner is a routing answer and not a missing one: the
// page reaches the owner, who is the person that would have written the row.
func TestADutyNobodyHoldsReachesTheOwner(t *testing.T) {
	ctx, pool, n, _ := newNotifier(t)

	for _, waiting := range []notifier.Wait{
		{Row: "mis_a", Kind: notifier.KindDriftMismatch, Waiting: "a record disagrees with what runs",
			Holding: people.OfObligation(people.ObligationDriftDetector), Worse: true},
		{Row: "mis_b", Kind: notifier.KindOwnerFired, Waiting: "the owner's own judgment", Worse: true},
	} {
		events, err := n.Notify(ctx, waiting)
		if err != nil {
			t.Fatalf("Notify about %s: %v", waiting.Row, err)
		}
		if len(events) != 1 {
			t.Fatalf("the page about %s wrote %d event(s), want one to the owner", waiting.Row, len(events))
		}
		read, err := notifier.EventsFor(ctx, pool, waiting.Row)
		if err != nil {
			t.Fatalf("EventsFor: %v", err)
		}
		if len(read) != 1 || read[0].Reached != theOwner {
			t.Errorf("the page about %s reached %+v, want the owner", waiting.Row, read)
		}
	}
}

// TestAPageWidensExactlyOnceToTheOwner is the whole of the widening: there is no
// narrower first recipient and no second widening.
func TestAPageWidensExactlyOnceToTheOwner(t *testing.T) {
	ctx, pool, n, _ := newNotifier(t)

	holding := people.OfObligation(people.ObligationDriftDetector)
	if _, err := people.NewWriter(pool).Declare(ctx,
		record.Actor{Kind: record.KindHuman, Name: theOwner}, "sre", holding); err != nil {
		t.Fatalf("declaring who installed the drift detector: %v", err)
	}
	waiting := notifier.Wait{
		Row: "mis_widening", Kind: notifier.KindDriftMismatch,
		Waiting: "a record disagrees with what runs", Holding: holding, Worse: true,
	}

	// Nothing to widen before anything was delivered: a page is the sequence of events
	// on one wait, and one with no beginning is not a page.
	if _, err := n.Widen(ctx, waiting); !errors.Is(err, notifier.ErrNothingReached) {
		t.Errorf("Widen before any delivery = %v, want %v", err, notifier.ErrNothingReached)
	}
	if _, err := n.Answered(ctx, waiting, "sre"); !errors.Is(err, notifier.ErrNothingReached) {
		t.Errorf("Answered before any delivery = %v, want %v", err, notifier.ErrNothingReached)
	}

	if _, err := n.Notify(ctx, waiting); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	widened, err := n.Widen(ctx, waiting)
	if err != nil {
		t.Fatalf("Widen: %v", err)
	}
	if widened.ID == "" {
		t.Error("the widening wrote no row")
	}
	if _, err := n.Widen(ctx, waiting); !errors.Is(err, notifier.ErrAlreadyWidened) {
		t.Errorf("a second Widen = %v, want %v", err, notifier.ErrAlreadyWidened)
	}

	read, err := notifier.EventsFor(ctx, pool, waiting.Row)
	if err != nil {
		t.Fatalf("EventsFor: %v", err)
	}
	if len(read) != 2 {
		t.Fatalf("the page's sequence is %d event(s), want reached then widened: %+v", len(read), read)
	}
	if notifier.Event(read[0].Event) != notifier.EventReached || read[0].Reached != "sre" {
		t.Errorf("the first event is %+v, want reached to the holder", read[0])
	}
	if notifier.Event(read[1].Event) != notifier.EventWidened || read[1].Reached != theOwner {
		t.Errorf("the second event is %+v, want widened to the owner", read[1])
	}

	// Answered closes the sequence, naming who ended the wait.
	if _, err := n.Answered(ctx, waiting, "sre"); err != nil {
		t.Fatalf("Answered: %v", err)
	}
	read, err = notifier.EventsFor(ctx, pool, waiting.Row)
	if err != nil {
		t.Fatalf("EventsFor: %v", err)
	}
	last := read[len(read)-1]
	if notifier.Event(last.Event) != notifier.EventAnswered || last.Reached != "sre" {
		t.Errorf("the last event is %+v, want answered by the human who ended it", last)
	}
	if err := decisionlog.Verify(ctx, pool); err != nil {
		t.Errorf("the chain does not verify: %v", err)
	}
}

// TestThePageConditionIsSettledPerKind is what makes "nothing else fires one"
// mechanical wherever the design has already applied the condition, and leaves the
// condition itself as the test wherever it has not.
func TestThePageConditionIsSettledPerKind(t *testing.T) {
	ctx, _, n, _ := newNotifier(t)

	for kind, answer := range notifier.Kinds {
		waiting := notifier.Wait{Row: "row_" + string(kind), Kind: kind, Waiting: "something waits"}
		switch answer {
		case notifier.PagesNever:
			if events, err := n.Notify(ctx, waiting); err != nil || len(events) != 0 {
				t.Errorf("a %s wrote %d page event(s), %v; nothing live is worse until a human ends one",
					kind, len(events), err)
			}
			waiting.Worse = true
			if _, err := n.Notify(ctx, waiting); !errors.Is(err, notifier.ErrWorseRefused) {
				t.Errorf("a %s asserting the condition = %v, want %v", kind, err, notifier.ErrWorseRefused)
			}
		case notifier.PagesAlways:
			if _, err := n.Notify(ctx, waiting); !errors.Is(err, notifier.ErrWorseRefused) {
				t.Errorf("a %s denying the condition = %v, want %v", kind, err, notifier.ErrWorseRefused)
			}
			waiting.Worse = true
			if events, err := n.Notify(ctx, waiting); err != nil || len(events) != 1 {
				t.Errorf("a %s wrote %d page event(s), %v; the condition is met by definition",
					kind, len(events), err)
			}
		case notifier.PagesIfWorse:
			if events, err := n.Notify(ctx, waiting); err != nil || len(events) != 0 {
				t.Errorf("a %s with nothing live worse wrote %d page event(s), %v", kind, len(events), err)
			}
			waiting.Row += "_worse"
			waiting.Worse = true
			if events, err := n.Notify(ctx, waiting); err != nil || len(events) != 1 {
				t.Errorf("a %s with something live worse wrote %d page event(s), %v", kind, len(events), err)
			}
		}
	}

	// A kind this component does not know is refused rather than delivered under a
	// name of its own.
	if _, err := n.Notify(ctx, notifier.Wait{Row: "row_x", Kind: "invented", Waiting: "something"}); !errors.Is(err, notifier.ErrKindUnknown) {
		t.Errorf("an invented kind = %v, want %v", err, notifier.ErrKindUnknown)
	}
}

// TestAnIncompleteWaitIsRefused is the two things every wait names: what waits, and
// what it is waiting for.
func TestAnIncompleteWaitIsRefused(t *testing.T) {
	ctx, _, n, _ := newNotifier(t)

	for _, waiting := range []notifier.Wait{
		{Kind: notifier.KindGateDecision, Waiting: "something"},
		{Row: "dl_a", Kind: notifier.KindGateDecision},
	} {
		if _, err := n.Notify(ctx, waiting); !errors.Is(err, notifier.ErrWaitIncomplete) {
			t.Errorf("Notify(%+v) = %v, want %v", waiting, err, notifier.ErrWaitIncomplete)
		}
	}
}

// TestADeliveryThatFailedWritesNoRecord is the order the notifier keeps: the record
// says a page was delivered, so writing it before the delivery failed would say
// something that did not happen.
func TestADeliveryThatFailedWritesNoRecord(t *testing.T) {
	ctx, pool, _, _ := newNotifier(t)

	refused := errors.New("the pager is unreachable")
	channels := &recorder{refuse: refused}
	n, err := notifier.New(pool, decisionlog.NewWriter(pool), channels, theOwner)
	if err != nil {
		t.Fatalf("composing the notifier: %v", err)
	}

	_, err = n.Notify(ctx, notifier.Wait{
		Row: "mis_unreachable", Kind: notifier.KindDriftMismatch,
		Waiting: "a record disagrees with what runs", Worse: true,
	})
	if !errors.Is(err, refused) {
		t.Errorf("Notify over a channel that refused = %v, want the channel's own error", err)
	}
	rows, err := decisionlog.Read(ctx, pool)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("the log holds %d rows after a delivery that failed", len(rows))
	}
}

// TestANotifierWithNoOwnerOrNoChannelIsRefused is what the component cannot be
// composed without: a page widens to the owner, and a notifier with nothing to deliver
// on delivers nothing.
func TestANotifierWithNoOwnerOrNoChannelIsRefused(t *testing.T) {
	_, pool, _, channels := newNotifier(t)
	log := decisionlog.NewWriter(pool)

	if _, err := notifier.New(pool, log, channels, ""); err == nil {
		t.Error("a notifier with no owner was composed, and a page widens to one")
	}
	if _, err := notifier.New(pool, log, nil, theOwner); err == nil {
		t.Error("a notifier with no channel was composed")
	}
}

// TestEventsForSkipsAPayloadItCannotRead is what every reader of this log does with a
// row some other component wrote: a payload is unconstrained bytes by decisionlog's
// contract, so a page event in a shape this package does not know is skipped and the
// sequence goes on.
func TestEventsForSkipsAPayloadItCannotRead(t *testing.T) {
	ctx, pool, n, _ := newNotifier(t)

	if _, err := decisionlog.NewWriter(pool).AppendPageEvent(ctx, decisionlog.Entry{
		Actor:   record.Actor{Kind: record.KindComponent, Name: "some.other.notifier"},
		Payload: "a payload this package has no shape for",
	}); err != nil {
		t.Fatalf("appending the unreadable page event: %v", err)
	}

	waiting := notifier.Wait{
		Row: "mis_after", Kind: notifier.KindDriftMismatch,
		Waiting: "a record disagrees with what runs", Worse: true,
	}
	if _, err := n.Notify(ctx, waiting); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	read, err := notifier.EventsFor(ctx, pool, waiting.Row)
	if err != nil {
		t.Fatalf("EventsFor over a row it cannot read: %v", err)
	}
	if len(read) != 1 {
		t.Errorf("the page's sequence is %d event(s), want the one this package wrote", len(read))
	}
}
