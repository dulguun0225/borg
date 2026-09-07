// The page as a sequence of events: who the first delivery reaches, the one
// widening to the owner, and the acknowledgement that stops it. The events are
// rows of the decision log, so this reaches a database. db_test.go holds what
// qualifies for a page at all and what the channels write; fixtures_test.go
// holds the notifier and the People writer these tests are composed over, and
// the small reads made directly against the log and this package's own
// delivery table.
//
// These tests do not skip when the database is unreachable — the milestone is
// demonstrated by them running, so an unreachable database fails the run.
package notifier_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/record"
)

// TestAPageWritesOneReachedEventPerHolder is the first delivery: it reaches every
// key holding the duty at once, there being no rotation naming which one it reaches
// first.
func TestAPageWritesOneReachedEventPerHolder(t *testing.T) {
	ctx, pool, token, n, channels := newNotifier(t)

	holding := people.OfDuty(12)
	writer := peopleWriter(pool, token)
	for _, key := range []string{"hk_ada", "hk_grace"} {
		if _, err := writer.Declare(ctx, theHumanOwner, key, holding); err != nil {
			t.Fatalf("declaring that %s holds %s: %v", key, holding, err)
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
		t.Fatalf("the page wrote %d event(s), and two keys hold the duty", len(events))
	}
	if channels.on(notifier.ChannelPage) != 2 {
		t.Errorf("the page went out %d times, want one per holder", channels.on(notifier.ChannelPage))
	}

	read, err := n.EventsFor(ctx, waiting.Row)
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
	if !reached["hk_ada"] || !reached["hk_grace"] {
		t.Errorf("the page reached %v, want both holder keys and never a name", reached)
	}

	// The rows are the log's, written as the notifier and not as whoever created the
	// wait: who created it is on that wait's own record.
	for _, row := range readLog(t, ctx, pool, token) {
		if row.Shape != decisionlog.ShapePageEvent && row.Shape != decisionlog.ShapeReadEvent {
			t.Errorf("row %s is shape %s, and a page event is its own shape", row.ID, row.Shape)
		}
		if row.Shape == decisionlog.ShapePageEvent && row.Actor != notifier.Actor {
			t.Errorf("row %s was written as %+v, want the notifier", row.ID, row.Actor)
		}
	}
	verifyLog(t, ctx, pool, token)
}

// TestEveryPageEventNamesTheServiceTheWaitIsAbout: the service id is on the
// payload, so a reader counting pages per service reads the page's own events
// and not the record of whatever was waiting. The acknowledgement is the case
// that matters — its caller has the row and nothing else, and the id is read
// off the reached event the way the kind and the words already are, which is
// what the wait it is called with naming no service is here to show.
func TestEveryPageEventNamesTheServiceTheWaitIsAbout(t *testing.T) {
	ctx, pool, token, n, _ := newNotifier(t)

	waiting := notifier.Wait{
		Row: "dm_1", Kind: notifier.KindDriftMismatch,
		Waiting: "what runs is not what the record names", Worse: true,
		ServiceID: "svc_a",
	}
	if _, err := n.Notify(ctx, waiting); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	acknowledging := notifier.Wait{
		Row: waiting.Row, Kind: waiting.Kind, Waiting: waiting.Waiting, Worse: true,
	}
	if _, err := n.Acknowledge(ctx, acknowledging, "hk_ada"); err != nil {
		t.Fatalf("Acknowledge over a wait naming no service: %v", err)
	}
	if _, err := n.Answered(ctx, waiting, "hk_ada"); err != nil {
		t.Fatalf("Answered: %v", err)
	}

	read, err := n.EventsFor(ctx, waiting.Row)
	if err != nil {
		t.Fatalf("EventsFor: %v", err)
	}
	if len(read) != 3 {
		t.Fatalf("the page's sequence is %d event(s) long, want the reached, the acknowledgement and the answer", len(read))
	}
	for _, e := range read {
		if e.ServiceID != waiting.ServiceID {
			t.Errorf("the %s event names service %q, want %q", e.Event, e.ServiceID, waiting.ServiceID)
		}
	}

	// The field is the second format version of this shape, so every event
	// written here declares that one.
	for _, row := range readLog(t, ctx, pool, token) {
		if row.Shape != decisionlog.ShapePageEvent {
			continue
		}
		if row.FormatVersion != notifier.PageEventFormatVersion {
			t.Errorf("page event %s declares %q, want %q", row.ID, row.FormatVersion, notifier.PageEventFormatVersion)
		}
	}
	verifyLog(t, ctx, pool, token)
}

// TestADutyNobodyHoldsReachesTheOwner is a routing answer and not a missing one: the
// page reaches the owner, who is the person that would have written the row.
func TestADutyNobodyHoldsReachesTheOwner(t *testing.T) {
	ctx, _, _, n, _ := newNotifier(t)

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
		read, err := n.EventsFor(ctx, waiting.Row)
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
	ctx, pool, token, n, _ := newNotifier(t)

	holding := people.OfObligation(people.ObligationDriftDetector)
	if _, err := peopleWriter(pool, token).Declare(ctx, theHumanOwner, "hk_sre", holding); err != nil {
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
	if _, err := n.Answered(ctx, waiting, "hk_sre"); !errors.Is(err, notifier.ErrNothingReached) {
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

	read, err := n.EventsFor(ctx, waiting.Row)
	if err != nil {
		t.Fatalf("EventsFor: %v", err)
	}
	if len(read) != 2 {
		t.Fatalf("the page's sequence is %d event(s), want reached then widened: %+v", len(read), read)
	}
	if notifier.Event(read[0].Event) != notifier.EventReached || read[0].Reached != "hk_sre" {
		t.Errorf("the first event is %+v, want reached to the holder", read[0])
	}
	if notifier.Event(read[1].Event) != notifier.EventWidened || read[1].Reached != theOwner {
		t.Errorf("the second event is %+v, want widened to the owner", read[1])
	}

	// Answered closes the sequence, naming who ended the wait.
	if _, err := n.Answered(ctx, waiting, "hk_sre"); err != nil {
		t.Fatalf("Answered: %v", err)
	}
	read, err = n.EventsFor(ctx, waiting.Row)
	if err != nil {
		t.Fatalf("EventsFor: %v", err)
	}
	last := read[len(read)-1]
	if notifier.Event(last.Event) != notifier.EventAnswered || last.Reached != "hk_sre" {
		t.Errorf("the last event is %+v, want answered by the human who ended it", last)
	}

	// A second page on the same row, after the answer, widens like the first:
	// once per page and not once per row, which is what a fixed row needs.
	if _, err := n.Notify(ctx, waiting); err != nil {
		t.Fatalf("Notify, second page: %v", err)
	}
	if _, err := n.Widen(ctx, waiting); err != nil {
		t.Fatalf("Widen on the second page = %v, want the widening", err)
	}
	if _, err := n.Widen(ctx, waiting); !errors.Is(err, notifier.ErrAlreadyWidened) {
		t.Errorf("a second Widen on the second page = %v, want %v", err, notifier.ErrAlreadyWidened)
	}
	verifyLog(t, ctx, pool, token)
}

// TestAcknowledgeStopsTheWideningAndNotASecondTime is the fourth event: it
// decides nothing and ends no wait, and it stops only the widening.
func TestAcknowledgeStopsTheWideningAndNotASecondTime(t *testing.T) {
	ctx, _, _, n, _ := newNotifier(t)

	waiting := notifier.Wait{
		Row: "dl_ack", Kind: notifier.KindOwnerFired,
		Waiting: "the owner's own judgment", Worse: true,
	}
	if _, err := n.Notify(ctx, waiting); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if _, err := n.Acknowledge(ctx, waiting, "hk_alice"); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	if _, err := n.Widen(ctx, waiting); !errors.Is(err, notifier.ErrAcknowledged) {
		t.Errorf("Widen after an acknowledgement = %v, want %v", err, notifier.ErrAcknowledged)
	}

	// The row still waits: acknowledging it does not answer it, and a
	// second acknowledgement from another holder is still accepted.
	if _, err := n.Acknowledge(ctx, waiting, "hk_bob"); err != nil {
		t.Fatalf("a second Acknowledge from another holder: %v", err)
	}

	if _, err := n.Answered(ctx, waiting, "owner"); err != nil {
		t.Fatalf("Answered: %v", err)
	}
	if _, err := n.Acknowledge(ctx, waiting, "hk_alice"); !errors.Is(err, notifier.ErrAlreadyAnswered) {
		t.Errorf("Acknowledge after Answered = %v, want %v", err, notifier.ErrAlreadyAnswered)
	}
}

// TestAcknowledgingTakesTheKindOffThePageAndARowThatPagesNobodyTakesItToo is
// the acknowledgement as Pages has it: the same act on a row that is a decision
// appends an acknowledgement to the decision as well, one act writing both, and
// a gate row that pages nobody still takes the acknowledgement — where the half
// this component writes is nothing. The caller acknowledging holds the row and
// not what the page was about, so the event carries the kind it was reached
// under.
func TestAcknowledgingTakesTheKindOffThePageAndARowThatPagesNobodyTakesItToo(t *testing.T) {
	ctx, _, _, n, channels := newNotifier(t)

	// The gate component's own call: it knows the row a human acknowledged at
	// Work and names the one kind of wait every firing leaves.
	asTheGateCalls := notifier.Wait{
		Row: "dl_open", Kind: notifier.KindGateDecision,
		Waiting: "a human at Work acknowledged the row this page was about",
		Holding: people.OfDuty(12),
	}
	if _, err := n.Notify(ctx, asTheGateCalls); err != nil {
		t.Fatalf("Notify of a row that pages nobody: %v", err)
	}
	row, err := n.Acknowledge(ctx, asTheGateCalls, "hk_alice")
	if err != nil {
		t.Fatalf("Acknowledge of a row that pages nobody: %v", err)
	}
	if row.ID != "" {
		t.Errorf("acknowledging a row no page reached wrote %s, want no page event", row.ID)
	}
	if channels.on(notifier.ChannelPage) != 0 {
		t.Errorf("a row that pages nobody reached the page channel %d time(s)", channels.on(notifier.ChannelPage))
	}

	// The same row under a kind that does page: the event names what the page
	// was reached under and not what this caller could name.
	paging := asTheGateCalls
	paging.Row, paging.Kind, paging.Worse = "dl_revert", notifier.KindGateRevertDecision, true
	paging.Waiting = "a revert waits at a gate while the rollback still holds"
	if _, err := n.Notify(ctx, paging); err != nil {
		t.Fatalf("Notify of a revert decision that pages: %v", err)
	}
	acknowledging := asTheGateCalls
	acknowledging.Row = paging.Row
	if _, err := n.Acknowledge(ctx, acknowledging, "hk_alice"); err != nil {
		t.Fatalf("Acknowledge of a revert decision: %v", err)
	}
	events, err := n.EventsFor(ctx, paging.Row)
	if err != nil {
		t.Fatalf("EventsFor: %v", err)
	}
	last := events[len(events)-1]
	if notifier.Event(last.Event) != notifier.EventAcknowledged {
		t.Fatalf("the last event on the revert's row is %+v, want the acknowledgement", last)
	}
	if last.WaitKind != string(notifier.KindGateRevertDecision) {
		t.Errorf("the acknowledgement names kind %q, want the kind the page was reached under", last.WaitKind)
	}
}

// TestEventsForSkipsAPayloadItCannotRead is what every reader of this log does with a
// row some other component wrote: a payload is unconstrained bytes by decisionlog's
// contract, so a page event in a shape this package does not know is skipped and the
// sequence goes on.
func TestEventsForSkipsAPayloadItCannotRead(t *testing.T) {
	ctx, pool, token, n, _ := newNotifier(t)

	if _, err := decisionlog.NewWriter(pool, token).AppendPageEvent(ctx, decisionlog.Entry{
		Actor:         record.Actor{Kind: record.KindComponent, Key: "some.other.notifier", Basis: record.BasisClaimed},
		Payload:       "a payload this package has no shape for",
		FormatVersion: notifier.PageEventFormatVersion,
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
	read, err := n.EventsFor(ctx, waiting.Row)
	if err != nil {
		t.Fatalf("EventsFor over a row it cannot read: %v", err)
	}
	if len(read) != 1 {
		t.Errorf("the page's sequence is %d event(s), want the one this package wrote", len(read))
	}
}
