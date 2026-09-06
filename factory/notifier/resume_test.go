// The notifier's restart: the delivery record it overwrites per row the log
// still holds open.
package notifier_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/people"
)

// TestResumeDeliversARowStillWaitingAgain is the restart doing its work: a row
// the log holds open is delivered again at the next start, and the delivery
// record is overwritten rather than added to — one row per waiting row and
// channel, which is what makes a restart idempotent.
func TestResumeDeliversARowStillWaitingAgain(t *testing.T) {
	ctx, pool, token, n, channels := newNotifier(t)

	opened, err := decisionlog.NewWriter(pool, token).AppendWaitOpen(ctx, decisionlog.Entry{
		Actor: testActor, Payload: `{"kind":"test"}`, FormatVersion: "wait/1",
	})
	if err != nil {
		t.Fatalf("opening the wait: %v", err)
	}
	if _, err := n.Notify(ctx, notifier.Wait{
		Row: opened.ID, Kind: notifier.KindItemEscalated,
		Waiting: "the factory gave up on this one", Holding: people.OfDuty(12), Worse: true,
	}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	first := len(channels.delivered)
	if first == 0 {
		t.Fatal("nothing was delivered, and the restart is about what was")
	}

	delivered, err := n.Resume(ctx)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if len(delivered) == 0 {
		t.Fatal("the restart delivered nothing, and the row is still open")
	}
	for _, row := range delivered {
		if row != opened.ID {
			t.Errorf("the restart delivered %s, want the row still waiting", row)
		}
	}
	if len(channels.delivered) <= first {
		t.Error("the restart reached no channel, and a row still waiting is delivered again")
	}
	if recipient, _, found := deliveryRow(t, ctx, pool, opened.ID, notifier.ChannelMail); !found || recipient == "" {
		t.Errorf("the delivery record of %s on mail reads back as %q found=%v", opened.ID, recipient, found)
	}
	verifyLog(t, ctx, pool, token)
}

// TestResumeLeavesARowThatStoppedWaiting: a row a closing ended is not
// delivered again, so a restart says nothing about work a human already
// finished.
func TestResumeLeavesARowThatStoppedWaiting(t *testing.T) {
	ctx, pool, token, n, channels := newNotifier(t)
	log := decisionlog.NewWriter(pool, token)

	opened, err := log.AppendWaitOpen(ctx, decisionlog.Entry{
		Actor: testActor, Payload: `{"kind":"test"}`, FormatVersion: "wait/1",
	})
	if err != nil {
		t.Fatalf("opening the wait: %v", err)
	}
	if _, err := n.Notify(ctx, notifier.Wait{
		Row: opened.ID, Kind: notifier.KindItemEscalated,
		Waiting: "the factory gave up on this one", Holding: people.OfDuty(12), Worse: true,
	}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if _, err := log.AppendWaitClose(ctx, decisionlog.Entry{
		Actor: testActor, Payload: `{"kind":"test"}`, FormatVersion: "wait/1", Closes: opened.ID,
	}); err != nil {
		t.Fatalf("closing the wait: %v", err)
	}
	before := len(channels.delivered)

	delivered, err := n.Resume(ctx)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if len(delivered) != 0 {
		t.Errorf("the restart delivered %v, and that row stopped waiting", delivered)
	}
	if len(channels.delivered) != before {
		t.Error("the restart reached a channel about a row nobody is waiting on")
	}
}
