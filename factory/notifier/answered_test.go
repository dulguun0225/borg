// The acknowledgement and the answer: the two page events that end no wait
// but stop the widening, and the one that says the wait is over, on the
// notifier's own transaction and on a caller's own. Split out of
// pageevents_test.go, which holds the reached event and the widening, to
// keep both files under the line bound.
//
// These tests do not skip when the database is unreachable — the milestone is
// demonstrated by them running, so an unreachable database fails the run.
package notifier_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/people"
)

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

// TestAcknowledgeDeliversNothing is finding 3's own fix: acknowledging a page
// is an act in the product and nothing else, and writes the page event
// without handing anything to the [notifier.Deliverer].
func TestAcknowledgeDeliversNothing(t *testing.T) {
	ctx, _, _, n, channels := newNotifier(t)

	waiting := notifier.Wait{
		Row: "dl_ack_delivers_nothing", Kind: notifier.KindOwnerFired,
		Waiting: "the owner's own judgment", Worse: true,
	}
	if _, err := n.Notify(ctx, waiting); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	before := len(channels.delivered)

	if _, err := n.Acknowledge(ctx, waiting, "hk_alice"); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	if len(channels.delivered) != before {
		t.Errorf("Acknowledge reached the deliverer %d time(s), want none: %+v",
			len(channels.delivered)-before, channels.delivered[before:])
	}

	events, err := n.EventsFor(ctx, waiting.Row)
	if err != nil {
		t.Fatalf("EventsFor: %v", err)
	}
	last := events[len(events)-1]
	if notifier.Event(last.Event) != notifier.EventAcknowledged {
		t.Fatalf("the last event is %+v, want the acknowledgement written even though nothing was delivered", last)
	}
}

// TestAnsweredDeliversNothing is finding 4's own fix: the component that ends
// the wait calls the notifier at the same write it ends it with, and what
// that write does is append the answered event — never a page to the human
// who ended it, the wait already being over.
func TestAnsweredDeliversNothing(t *testing.T) {
	ctx, _, _, n, channels := newNotifier(t)

	waiting := notifier.Wait{
		Row: "dl_answered_delivers_nothing", Kind: notifier.KindOwnerFired,
		Waiting: "the owner's own judgment", Worse: true,
	}
	if _, err := n.Notify(ctx, waiting); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	before := len(channels.delivered)

	if _, err := n.Answered(ctx, waiting, "hk_alice"); err != nil {
		t.Fatalf("Answered: %v", err)
	}
	if len(channels.delivered) != before {
		t.Errorf("Answered reached the deliverer %d time(s), want none: %+v",
			len(channels.delivered)-before, channels.delivered[before:])
	}

	events, err := n.EventsFor(ctx, waiting.Row)
	if err != nil {
		t.Fatalf("EventsFor: %v", err)
	}
	last := events[len(events)-1]
	if notifier.Event(last.Event) != notifier.EventAnswered || last.Reached != "hk_alice" {
		t.Fatalf("the last event is %+v, want the answer written even though nothing was delivered", last)
	}
}

// TestAnswerTxWritesOnTheCallersTransaction is finding 4's other half:
// [notifier.Notifier.AnswerTx] appends the answered event on a transaction
// the caller already holds open, and delivers nothing, the same as
// [notifier.Notifier.Answered].
func TestAnswerTxWritesOnTheCallersTransaction(t *testing.T) {
	ctx, pool, _, n, channels := newNotifier(t)

	waiting := notifier.Wait{
		Row: "dl_answer_tx", Kind: notifier.KindOwnerFired,
		Waiting: "the owner's own judgment", Worse: true,
	}
	if _, err := n.Notify(ctx, waiting); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	before := len(channels.delivered)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := n.AnswerTx(ctx, tx, waiting, "hk_alice"); err != nil {
		t.Fatalf("AnswerTx: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if len(channels.delivered) != before {
		t.Errorf("AnswerTx reached the deliverer %d time(s), want none: %+v",
			len(channels.delivered)-before, channels.delivered[before:])
	}
	events, err := n.EventsFor(ctx, waiting.Row)
	if err != nil {
		t.Fatalf("EventsFor: %v", err)
	}
	last := events[len(events)-1]
	if notifier.Event(last.Event) != notifier.EventAnswered || last.Reached != "hk_alice" {
		t.Fatalf("the last event is %+v, want the answer written on the caller's transaction", last)
	}
}

// TestAnswerTxRollsBackWithTheCallersTransaction is what "at the same write
// it ends it with" means: the answered event lands only where the caller's
// own transaction commits, so a caller whose ending write fails leaves no
// answered event behind it either.
func TestAnswerTxRollsBackWithTheCallersTransaction(t *testing.T) {
	ctx, pool, _, n, _ := newNotifier(t)

	waiting := notifier.Wait{
		Row: "dl_answer_tx_rollback", Kind: notifier.KindOwnerFired,
		Waiting: "the owner's own judgment", Worse: true,
	}
	if _, err := n.Notify(ctx, waiting); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := n.AnswerTx(ctx, tx, waiting, "hk_alice"); err != nil {
		t.Fatalf("AnswerTx: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	events, err := n.EventsFor(ctx, waiting.Row)
	if err != nil {
		t.Fatalf("EventsFor: %v", err)
	}
	last := events[len(events)-1]
	if notifier.Event(last.Event) == notifier.EventAnswered {
		t.Fatalf("the answered event survived a rollback of the transaction it was appended on: %+v", last)
	}
}
