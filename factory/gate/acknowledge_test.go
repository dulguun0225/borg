// [gate.Gate.Acknowledge]: a holder of the row's duty saying at Work that they
// have the row. It decides nothing and excludes nobody, it is refused for an
// actor that is not a human, and where the row also pages it calls the
// notifier for the page's acknowledged event.
package gate_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/record"
)

func TestAcknowledgeAppendsAndRefusesANonHuman(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, _, _, g := newGate(t, s, p)

	opened, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}

	row, err := g.Acknowledge(ctx, opened, owner)
	if err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	if row.Closes != opened.Row.ID {
		t.Errorf("the acknowledgement closes %q, want %q", row.Closes, opened.Row.ID)
	}
	if row.Actor != owner {
		t.Errorf("the acknowledgement's actor is %+v, want %+v", row.Actor, owner)
	}

	component := record.Actor{Kind: record.KindComponent, Key: "gate.merge_to_master", Basis: record.BasisClaimed}
	if _, err := g.Acknowledge(ctx, opened, component); !errors.Is(err, decisionlog.ErrAcknowledgementNotHuman) {
		t.Errorf("Acknowledge by a component = %v, want ErrAcknowledgementNotHuman", err)
	}

	// A second acknowledgement by the same human is refused as a second close
	// is, by the store's own constraint.
	if _, err := g.Acknowledge(ctx, opened, owner); err == nil {
		t.Error("a second acknowledgement by the same human was accepted")
	}
}

// TestAcknowledgeOfAPagingRowCallsTheNotifier: where the row also pages — a
// human deciding on a revert while the rollback that removed the defect still
// holds, which is the one condition that does — one act at Work writes both
// the acknowledgement row and the page's acknowledged event.
func TestAcknowledgeOfAPagingRowCallsTheNotifier(t *testing.T) {
	notifier := &fakeNotifier{}
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, token, g := newGateWith(t, s, p, func(c *gate.Composition) {
		c.Notifier = notifier
	})

	firing := deployFiring(t, ctx, pool, token)
	firing.RevertWhileRollbackHolds = true
	opened, err := g.Fire(ctx, firing)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if !opened.Pages() {
		t.Fatal("a firing with a revert decided while the rollback holds does not page")
	}

	if _, err := g.Acknowledge(ctx, opened, owner); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	if len(notifier.acknowledged) != 1 || notifier.acknowledged[0] != opened.Row.ID {
		t.Errorf("the notifier was told %v acknowledged, want %q", notifier.acknowledged, opened.Row.ID)
	}
}

// TestAcknowledgeOfAMismatchDoesNotCallTheNotifier: a mismatch the drift
// detector found puts a human at the row, but the row it holds pages nobody, so
// an acknowledgement of it calls the notifier for nothing — the detector's own
// page is reached through its own sweep and not through this row.
func TestAcknowledgeOfAMismatchDoesNotCallTheNotifier(t *testing.T) {
	notifier := &fakeNotifier{}
	s, p := &fakeScore{assessment: assessed(0.2)}, &fakePolicy{applied: applied(0.5)}
	ctx, pool, token, g := newGateWith(t, s, p, func(c *gate.Composition) {
		c.DriftDetector = fakeDrift{found: true, why: "the release does not match what runs"}
		c.Notifier = notifier
	})

	opened, err := g.Fire(ctx, deployFiring(t, ctx, pool, token))
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if opened.Mismatch == "" {
		t.Fatal("the firing found no mismatch to hold the row on")
	}
	if opened.Pages() {
		t.Fatal("a mismatch holds the row and pages nobody, and this firing pages")
	}

	if _, err := g.Acknowledge(ctx, opened, owner); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}
	if len(notifier.acknowledged) != 0 {
		t.Errorf("the notifier was told %v acknowledged, want nothing: the row it was held on pages nobody", notifier.acknowledged)
	}
}
