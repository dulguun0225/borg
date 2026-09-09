// person_test.go is [notifier.Wait.Person]: the named human a wait routes to
// ahead of [notifier.Wait.Holding], and empty where the raiser has none.
package notifier_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/people"
)

// TestAWaitNamingAPersonReachesThemAndNotTheOwner is finding 1's own fix: a
// wait naming nobody's duty and no holder reaches [notifier.Wait.Person]
// instead of widening straight to the owner.
func TestAWaitNamingAPersonReachesThemAndNotTheOwner(t *testing.T) {
	ctx, _, _, n, channels := newNotifier(t)

	waiting := notifier.Wait{
		Row: "ceiling_hk_ada", Kind: notifier.KindSpendCeilingFraction,
		Waiting: "a spend ceiling notice", Person: "hk_ada",
	}
	if _, err := n.Notify(ctx, waiting); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(channels.delivered) == 0 {
		t.Fatal("nothing was delivered")
	}
	for _, d := range channels.delivered {
		if d.To != "hk_ada" {
			t.Errorf("a delivery reached %q, want the named person and not the owner", d.To)
		}
	}
}

// TestAPersonRoutesAheadOfADuty exercises the case a wait names both: the
// named human is reached and not the duty's holders, since a raiser's own
// fact about who this row is for is more specific than the duty it happens
// to fall under.
func TestAPersonRoutesAheadOfADuty(t *testing.T) {
	ctx, pool, token, n, channels := newNotifier(t)

	holding := people.OfDuty(12)
	writer := peopleWriter(pool, token)
	if _, err := writer.Declare(ctx, theHumanOwner, "hk_holder", holding); err != nil {
		t.Fatalf("declaring who holds %s: %v", holding, err)
	}

	waiting := notifier.Wait{
		Row: "dl_named_over_duty", Kind: notifier.KindGateRevertDecision,
		Waiting: "a revert waits at a gate while the rollback still holds",
		Holding: holding, Person: "hk_named", Worse: true,
	}
	if _, err := n.Notify(ctx, waiting); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if channels.on(notifier.ChannelPage) != 1 {
		t.Fatalf("the page went out %d time(s), want exactly one, to the named human",
			channels.on(notifier.ChannelPage))
	}
	for _, d := range channels.delivered {
		if d.To != "hk_named" {
			t.Errorf("a delivery reached %q, want the named human and not the duty's holder", d.To)
		}
	}
}

// TestAWaitNamingNoPersonRoutesOnTheDutyAsBefore is what stays true where the
// raiser has no such fact: routing falls back to the duty, or the owner
// where it belongs to none, exactly as it did before [notifier.Wait.Person]
// existed.
func TestAWaitNamingNoPersonRoutesOnTheDutyAsBefore(t *testing.T) {
	ctx, _, _, n, channels := newNotifier(t)

	waiting := notifier.Wait{
		Row: "dl_no_person", Kind: notifier.KindOwnerFired,
		Waiting: "the owner's own judgment", Worse: true,
	}
	if _, err := n.Notify(ctx, waiting); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	for _, d := range channels.delivered {
		if d.To != theOwner {
			t.Errorf("a delivery reached %q, want the owner", d.To)
		}
	}
}
