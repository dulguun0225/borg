// The notifier's restart: the delivery record it overwrites per row the log
// still holds open, and per row a kind with no log opening still waits on,
// read off that kind's own subject instead.
package notifier_test

import (
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/driftdetector"
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

	delivered, err := n.Resume(ctx, nil)
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

// TestResumeDeliversAKindThatPagesNeverAgain is finding 6's own fix: a row of
// a kind that pages never carries no page event to rebuild the wait from,
// and the delivery record itself is what redelivers it — mail and chat, both
// again — rather than the row being left waiting because there was no page
// event to read.
func TestResumeDeliversAKindThatPagesNeverAgain(t *testing.T) {
	ctx, pool, token, n, channels := newNotifier(t)

	opened, err := decisionlog.NewWriter(pool, token).AppendWaitOpen(ctx, decisionlog.Entry{
		Actor: testActor, Payload: `{"kind":"test"}`, FormatVersion: "wait/1",
	})
	if err != nil {
		t.Fatalf("opening the wait: %v", err)
	}
	if _, err := n.Notify(ctx, notifier.Wait{
		Row: opened.ID, Kind: notifier.KindGateDecision,
		Waiting: "a human decides at the merge row", Holding: people.OfDuty(12),
	}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if channels.on(notifier.ChannelPage) != 0 {
		t.Fatal("a gate decision paged, and nothing live is worse until a human closes one")
	}
	before := len(channels.delivered)

	delivered, err := n.Resume(ctx, nil)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if len(delivered) != 1 || delivered[0] != opened.ID {
		t.Fatalf("Resume delivered %v, want the row still waiting even though no page ever reached it", delivered)
	}
	if len(channels.delivered) != before+2 {
		t.Errorf("the restart reached the deliverer %d time(s), want mail and chat delivered again",
			len(channels.delivered)-before)
	}
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

	delivered, err := n.Resume(ctx, nil)
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

// TestResumeRedeliversAnUnclearedMismatchAndLeavesAClearedOne is C2765 for a
// kind with no log opening: a drift mismatch never opens in the decision log,
// so "still waiting" is read off the drift detector's own store instead —
// uncleared is redelivered, and cleared is left alone.
func TestResumeRedeliversAnUnclearedMismatchAndLeavesAClearedOne(t *testing.T) {
	ctx, pool, token, n, channels := newNotifier(t)
	if _, err := peopleWriter(pool, token).Declare(ctx, theHumanOwner, "hk_sre",
		people.OfObligation(people.ObligationDriftDetector)); err != nil {
		t.Fatalf("declaring who installed the drift detector: %v", err)
	}

	drift := driftTestPool(t, ctx)
	recorded, err := driftdetector.NewWriter(drift).Record(ctx, driftdetector.Pass{
		ServiceID: "svc_1", Target: "t1", Reached: true, RunningBuild: "b_running",
		RecordedBuildID: "b_recorded", Interval: time.Minute,
	})
	if err != nil || recorded.Raised == "" {
		t.Fatalf("recording a mismatch: raised %q, %v", recorded.Raised, err)
	}
	if _, err := n.Notify(ctx, notifier.Wait{
		Row: recorded.Raised, Kind: notifier.KindDriftMismatch,
		Waiting: "a record disagrees with what runs", Worse: true, ServiceID: "svc_1",
		Holding: people.OfObligation(people.ObligationDriftDetector),
	}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	before := len(channels.delivered)

	// Uncleared: the decision log never opened this row, so a restart that
	// only read the log would leave it undelivered. Reading the mismatch's
	// own store instead finds it still uncleared and delivers it again.
	delivered, err := n.Resume(ctx, drift)
	if err != nil {
		t.Fatalf("Resume (uncleared): %v", err)
	}
	if len(delivered) != 1 || delivered[0] != recorded.Raised {
		t.Fatalf("Resume delivered %v, want the uncleared mismatch delivered again", delivered)
	}
	if len(channels.delivered) <= before {
		t.Error("the restart reached no channel for a mismatch still uncleared")
	}

	// Cleared at the detector's own store, which calls nothing: a restart
	// after that reads the mismatch as no longer waiting.
	if _, err := driftdetector.NewWriter(drift).Clear(ctx, recorded.Raised, "hk_sre",
		"the target was redeployed by hand"); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	after := len(channels.delivered)
	delivered, err = n.Resume(ctx, drift)
	if err != nil {
		t.Fatalf("Resume (cleared): %v", err)
	}
	if len(delivered) != 0 {
		t.Errorf("Resume delivered %v, and the mismatch is cleared", delivered)
	}
	if len(channels.delivered) != after {
		t.Error("the restart reached a channel about a mismatch already cleared")
	}
}
