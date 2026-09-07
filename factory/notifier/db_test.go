// What qualifies a wait for a page rather than for mail, what a wait has to
// name, and what each channel writes: mail and chat write a delivery record
// and nothing else, and a refused send writes one too. pageevents_test.go
// holds the four events a page is a sequence of, and driftpass_test.go the
// notifier's own last check and its reads of the drift detector's store.
// fixtures_test.go holds the notifier and the People writer these tests are
// composed over, and the small reads made directly against the log and this
// package's own delivery table.
//
// These tests do not skip when the database is unreachable — the milestone is
// demonstrated by them running, so an unreachable database fails the run.
package notifier_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/notifier"
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
