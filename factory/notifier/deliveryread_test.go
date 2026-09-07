// deliveryread_test.go is [notifier.DeliveryRecord.FirstAcceptedAt] and
// [notifier.DeliveriesOf]: the first accepted time surviving a refusal
// before it and a refusal after it, staying put across two acceptances, and
// the read across every channel and recipient of one row. Split out of
// delivery_test.go so that file stays under the line bound.
package notifier_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/notifier"
)

// errToggleRefused is what [toggle] returns for a call it is told to refuse.
var errToggleRefused = errors.New("notifier_test: the toggled channel refused this attempt")

// toggle is a [notifier.Deliverer] that refuses one channel on chosen
// attempts and accepts it on every other, which is what lets a test drive a
// channel through a chosen sequence of refusals and acceptances. Every other
// channel always accepts.
type toggle struct {
	channel  notifier.Channel
	refuseAt map[int]bool
	calls    int
}

func (s *toggle) Deliver(_ context.Context, d notifier.Delivery) error {
	if d.Channel != s.channel {
		return nil
	}
	s.calls++
	if s.refuseAt[s.calls] {
		return errToggleRefused
	}
	return nil
}

// findChannel is the one record of records on channel, failing the test if
// there is none.
func findChannel(t *testing.T, records []notifier.DeliveryRecord, channel notifier.Channel) notifier.DeliveryRecord {
	t.Helper()
	for _, r := range records {
		if r.Channel == channel {
			return r
		}
	}
	t.Fatalf("no delivery record on %s among %+v", channel, records)
	return notifier.DeliveryRecord{}
}

// TestFirstAcceptedAtSurvivesARefusalBeforeAndAfterIt is the split the
// design asks for: refused, then accepted, then refused again reads
// FirstAcceptedAt equal to the second attempt's own time and
// TransportAccepted false, because the field names when the transport first
// accepted and not whether the latest attempt did.
func TestFirstAcceptedAtSurvivesARefusalBeforeAndAfterIt(t *testing.T) {
	ctx, pool, token, _, _ := newNotifier(t)
	channels := &toggle{channel: notifier.ChannelMail, refuseAt: map[int]bool{1: true, 3: true}}
	n, err := notifier.New(pool, decisionlog.NewWriter(pool, token), token, channels, theOwner)
	if err != nil {
		t.Fatalf("composing the notifier: %v", err)
	}

	waiting := notifier.Wait{
		Row: "dl_first_accepted", Kind: notifier.KindGateDecision,
		Waiting: "a human decides at the merge row",
	}

	if _, err := n.Notify(ctx, waiting); !errors.Is(err, errToggleRefused) {
		t.Fatalf("attempt 1 = %v, want the mail channel's own refusal", err)
	}
	records, err := notifier.DeliveriesOf(ctx, pool, waiting.Row)
	if err != nil {
		t.Fatalf("DeliveriesOf after attempt 1: %v", err)
	}
	mail := findChannel(t, records, notifier.ChannelMail)
	if mail.TransportAccepted || mail.FirstAcceptedAt != "" {
		t.Fatalf("after a refused attempt, mail = accepted %t firstAcceptedAt %q, want false \"\"",
			mail.TransportAccepted, mail.FirstAcceptedAt)
	}

	time.Sleep(time.Millisecond)
	if _, err := n.Notify(ctx, waiting); err != nil {
		t.Fatalf("attempt 2: %v", err)
	}
	records, err = notifier.DeliveriesOf(ctx, pool, waiting.Row)
	if err != nil {
		t.Fatalf("DeliveriesOf after attempt 2: %v", err)
	}
	mail = findChannel(t, records, notifier.ChannelMail)
	if !mail.TransportAccepted || mail.FirstAcceptedAt == "" {
		t.Fatalf("after the accepted attempt, mail = accepted %t firstAcceptedAt %q, want true and set",
			mail.TransportAccepted, mail.FirstAcceptedAt)
	}
	if mail.FirstAcceptedAt != mail.At {
		t.Errorf("the first accepted attempt's own firstAcceptedAt = %q, want its own at %q", mail.FirstAcceptedAt, mail.At)
	}
	firstAccepted := mail.FirstAcceptedAt

	time.Sleep(time.Millisecond)
	if _, err := n.Notify(ctx, waiting); !errors.Is(err, errToggleRefused) {
		t.Fatalf("attempt 3 = %v, want the mail channel's own refusal", err)
	}
	records, err = notifier.DeliveriesOf(ctx, pool, waiting.Row)
	if err != nil {
		t.Fatalf("DeliveriesOf after attempt 3: %v", err)
	}
	mail = findChannel(t, records, notifier.ChannelMail)
	if mail.TransportAccepted {
		t.Errorf("after a second refusal, TransportAccepted = true, want false")
	}
	if mail.FirstAcceptedAt != firstAccepted {
		t.Errorf("FirstAcceptedAt = %q after a refusal following acceptance, want %q kept", mail.FirstAcceptedAt, firstAccepted)
	}
}

// TestFirstAcceptedAtKeepsTheFirstOfTwoAcceptedAttempts is a row accepted
// twice: FirstAcceptedAt keeps the first attempt's time and does not move to
// the second's.
func TestFirstAcceptedAtKeepsTheFirstOfTwoAcceptedAttempts(t *testing.T) {
	ctx, pool, _, n, _ := newNotifier(t)

	waiting := notifier.Wait{
		Row: "dl_accepted_twice", Kind: notifier.KindGateDecision,
		Waiting: "a human decides at the merge row",
	}
	if _, err := n.Notify(ctx, waiting); err != nil {
		t.Fatalf("attempt 1: %v", err)
	}
	records, err := notifier.DeliveriesOf(ctx, pool, waiting.Row)
	if err != nil {
		t.Fatalf("DeliveriesOf after attempt 1: %v", err)
	}
	firstAccepted := findChannel(t, records, notifier.ChannelMail).FirstAcceptedAt
	if firstAccepted == "" {
		t.Fatalf("the first accepted attempt left FirstAcceptedAt empty")
	}

	time.Sleep(time.Millisecond)
	if _, err := n.Notify(ctx, waiting); err != nil {
		t.Fatalf("attempt 2: %v", err)
	}
	records, err = notifier.DeliveriesOf(ctx, pool, waiting.Row)
	if err != nil {
		t.Fatalf("DeliveriesOf after attempt 2: %v", err)
	}
	mail := findChannel(t, records, notifier.ChannelMail)
	if mail.At == firstAccepted {
		t.Fatalf("the second attempt's own at coincides with the first accepted time; the test cannot tell them apart")
	}
	if mail.FirstAcceptedAt != firstAccepted {
		t.Errorf("FirstAcceptedAt = %q after a second accepted attempt, want %q kept", mail.FirstAcceptedAt, firstAccepted)
	}
}

// TestDeliveriesOfReturnsEveryChannelAndRecipient is the read the decision
// view needs: every channel a row reached, in the order the rows were
// written, and nothing for a row this component never delivered about.
func TestDeliveriesOfReturnsEveryChannelAndRecipient(t *testing.T) {
	ctx, pool, _, n, _ := newNotifier(t)

	waiting := notifier.Wait{
		Row: "dl_deliveries_of", Kind: notifier.KindGateDecision,
		Waiting: "a human decides at the merge row",
	}
	if _, err := n.Notify(ctx, waiting); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	records, err := notifier.DeliveriesOf(ctx, pool, waiting.Row)
	if err != nil {
		t.Fatalf("DeliveriesOf: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("DeliveriesOf returned %d record(s), want one for mail and one for chat", len(records))
	}
	seen := map[notifier.Channel]bool{}
	for _, r := range records {
		if r.RowID != waiting.Row {
			t.Errorf("a record names row %q, want %q", r.RowID, waiting.Row)
		}
		seen[r.Channel] = true
	}
	for _, channel := range []notifier.Channel{notifier.ChannelMail, notifier.ChannelChat} {
		if !seen[channel] {
			t.Errorf("DeliveriesOf did not return a record for %s", channel)
		}
	}

	none, err := notifier.DeliveriesOf(ctx, pool, "no_such_row")
	if err != nil {
		t.Fatalf("DeliveriesOf of an unknown row: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("DeliveriesOf(unknown row) = %d record(s), want none", len(none))
	}
}
