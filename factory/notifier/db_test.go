// The notifier's own tests: what qualifies for a page, who a delivery reaches, and
// the four events a page is a sequence of. The page events are rows of the decision
// log, so this reaches a database; mail and chat write a delivery record and
// nothing else, which is one of the things asserted here. fixtures_test.go holds
// the notifier and the People writer these tests are composed over, and the small
// reads made directly against the log and this package's own delivery table.
// driftpass_test.go holds the notifier's own last check and its reads of the
// drift detector's store.
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

// TestMailAndChatWriteADeliveryRecordAndNoLogRow is the narrow channel split
// from the other two: everything waiting on a human goes out on mail and
// chat, and both write a delivery record — "written on mail and chat too" —
// while only the log holds a page event, and a wait that does not qualify
// for one writes none.
func TestMailAndChatWriteADeliveryRecordAndNoLogRow(t *testing.T) {
	ctx, pool, token, n, channels := newNotifier(t)

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

	for _, channel := range []notifier.Channel{notifier.ChannelMail, notifier.ChannelChat} {
		recipient, accepted, found := deliveryRow(t, ctx, pool, waiting.Row, channel)
		if !found || recipient != theOwner || !accepted {
			t.Errorf("the delivery record for %s = recipient %q accepted %t found %t, want %q true true",
				channel, recipient, accepted, found, theOwner)
		}
	}

	rows := readLog(t, ctx, pool, token)
	if len(rows) != 1 || rows[0].Shape != decisionlog.ShapeReadEvent {
		t.Errorf("the log holds %+v after mail and chat, want only this read's own event", rows)
	}
}

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

// TestThePageConditionIsSettledPerKind is what makes "nothing else fires one"
// mechanical wherever the design has already applied the condition, and leaves the
// condition itself as the test wherever it has not.
func TestThePageConditionIsSettledPerKind(t *testing.T) {
	ctx, _, _, n, _ := newNotifier(t)

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

// TestGateRevertDecisionCanPage is finding 4's own fix: a gate decision on a
// revert, unlike an ordinary one, can leave production worse, so its own
// kind takes the condition rather than refusing it outright.
func TestGateRevertDecisionCanPage(t *testing.T) {
	ctx, _, _, n, _ := newNotifier(t)

	waiting := notifier.Wait{
		Row: "dl_revert", Kind: notifier.KindGateRevertDecision,
		Waiting: "a human decides on a revert while the rollback still holds", Worse: true,
	}
	events, err := n.Notify(ctx, waiting)
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("a revert-gate decision wrote %d page event(s), want one", len(events))
	}
}

// TestAnIncompleteWaitIsRefused is the two things every wait names: what waits, and
// what it is waiting for.
func TestAnIncompleteWaitIsRefused(t *testing.T) {
	ctx, _, _, n, _ := newNotifier(t)

	for _, waiting := range []notifier.Wait{
		{Kind: notifier.KindGateDecision, Waiting: "something"},
		{Row: "dl_a", Kind: notifier.KindGateDecision},
	} {
		if _, err := n.Notify(ctx, waiting); !errors.Is(err, notifier.ErrWaitIncomplete) {
			t.Errorf("Notify(%+v) = %v, want %v", waiting, err, notifier.ErrWaitIncomplete)
		}
	}
}

// TestADeliveryThatFailedWritesADeliveryRecordAndNoPageEvent is the split
// "written on mail and chat too, and on a refused send" makes possible: a
// row no delivery was ever accepted for is reported as a fault of the
// channel and against nobody, which needs the record even where the send
// failed.
func TestADeliveryThatFailedWritesADeliveryRecordAndNoPageEvent(t *testing.T) {
	ctx, pool, token, _, _ := newNotifier(t)

	refused := errors.New("the pager is unreachable")
	channels := &recorder{refuse: refused}
	n, err := notifier.New(pool, decisionlog.NewWriter(pool, token), token, channels, theOwner)
	if err != nil {
		t.Fatalf("composing the notifier: %v", err)
	}

	waiting := notifier.Wait{
		Row: "mis_unreachable", Kind: notifier.KindDriftMismatch,
		Waiting: "a record disagrees with what runs", Worse: true,
	}
	_, err = n.Notify(ctx, waiting)
	if !errors.Is(err, refused) {
		t.Errorf("Notify over a channel that refused = %v, want the channel's own error", err)
	}
	rows := readLog(t, ctx, pool, token)
	if len(rows) != 1 || rows[0].Shape != decisionlog.ShapeReadEvent {
		t.Errorf("the log holds %+v after a delivery that failed, want only this read's own event", rows)
	}
	recipient, accepted, found := deliveryRow(t, ctx, pool, waiting.Row, notifier.ChannelMail)
	if !found || accepted || recipient != theOwner {
		t.Errorf("the delivery record for the refused mail send = recipient %q accepted %t found %t, want %q false true",
			recipient, accepted, found, theOwner)
	}
}

// TestANotifierWithNoOwnerOrNoChannelIsRefused is what the component cannot be
// composed without: a page widens to the owner, and a notifier with nothing to deliver
// on delivers nothing.
func TestANotifierWithNoOwnerOrNoChannelIsRefused(t *testing.T) {
	_, pool, token, _, channels := newNotifier(t)
	log := decisionlog.NewWriter(pool, token)

	if _, err := notifier.New(pool, log, token, channels, ""); err == nil {
		t.Error("a notifier with no owner was composed, and a page widens to one")
	}
	if _, err := notifier.New(pool, log, token, nil, theOwner); err == nil {
		t.Error("a notifier with no channel was composed")
	}
}

// TestEventsForSkipsAPayloadItCannotRead is what every reader of this log does with a
// row some other component wrote: a payload is unconstrained bytes by decisionlog's
// contract, so a page event in a shape this package does not know is skipped and the
// sequence goes on.
func TestEventsForSkipsAPayloadItCannotRead(t *testing.T) {
	ctx, pool, token, n, _ := newNotifier(t)

	if _, err := decisionlog.NewWriter(pool, token).AppendPageEvent(ctx, decisionlog.Entry{
		Actor:         record.Actor{Kind: record.KindComponent, Key: "some.other.notifier"},
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
