// Check.ConsumerContractsInForce, the range from a service's last known-good
// release to its newest, and a service with no closed window having no last
// known-good release and every release in force.
package contractcheck_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/window"
)

// TestConsumerContractsInForceRunFromTheLastKnownGoodToTheNewest, and a service
// with no window closed passed or timed out has none and every release it has is
// in the range.
func TestConsumerContractsInForceRunFromTheLastKnownGoodToTheNewest(t *testing.T) {
	ctx, g := newGraph(t)

	// Three releases of the consumer: the first two declare an element the third
	// stops declaring, and only the second's window closes at the cap. The last
	// known-good release is the release the newest closed window watched, so
	// releases 2 and 3 are in force and release 1 is not.
	ship(t, ctx, g, g.consumer, nil, []consumercontract.Draft{
		draft(g.producer, theInterface, "Gone", gatepolicy.PredicateRead, ""),
	}, window.ExitFailed)
	second, secondWindow := ship(t, ctx, g, g.consumer, nil, []consumercontract.Draft{
		draft(g.producer, theInterface, "Detail", gatepolicy.PredicateRead, ""),
	}, window.ExitTimedOut)
	ship(t, ctx, g, g.consumer, nil, []consumercontract.Draft{
		draft(g.producer, theInterface, "Status", gatepolicy.PredicateRead, ""),
	}, "")

	in, err := g.check.ConsumerContractsInForce(ctx, g.consumer.ID)
	if err != nil {
		t.Fatalf("ConsumerContractsInForce: %v", err)
	}
	if !in.HasLastKnownGood || in.LastKnownGoodNumber != second.Number || in.LastKnownGoodWindowID != secondWindow {
		t.Fatalf("the last known-good release is %+v, want release %d set by window %s", in, second.Number, secondWindow)
	}
	if in.HighestNumber != 3 {
		t.Errorf("the newest release is %d, want three", in.HighestNumber)
	}
	if len(in.ItemIDs) != 2 {
		t.Fatalf("the range holds %d items, want the two from the last known-good release up", len(in.ItemIDs))
	}
	named := map[string]bool{}
	for _, p := range in.Predicates {
		named[p.Element] = true
	}
	if !named["Detail"] || !named["Status"] {
		t.Errorf("the consumer contracts in force name %v, want Detail and Status", named)
	}
	if named["Gone"] {
		t.Error("a release below the last known-good release is still in force, and a rollback cannot return to it")
	}
}

// TestAServiceWithNoClosedWindowHasNoLastKnownGoodAndEveryReleaseInForce: which is
// the direction a first release's missing rollback target already takes.
func TestAServiceWithNoClosedWindowHasNoLastKnownGoodAndEveryReleaseInForce(t *testing.T) {
	ctx, g := newGraph(t)

	ship(t, ctx, g, g.consumer, nil, []consumercontract.Draft{
		draft(g.producer, theInterface, "Status", gatepolicy.PredicateRead, ""),
	}, "")
	ship(t, ctx, g, g.consumer, nil, []consumercontract.Draft{
		draft(g.producer, theInterface, "Detail", gatepolicy.PredicateRead, ""),
	}, "")

	in, err := g.check.ConsumerContractsInForce(ctx, g.consumer.ID)
	if err != nil {
		t.Fatalf("ConsumerContractsInForce: %v", err)
	}
	if in.HasLastKnownGood {
		t.Fatalf("a service with no closed window has a last known-good release: %+v", in)
	}
	if len(in.ItemIDs) != 2 || len(in.Predicates) != 2 {
		t.Fatalf("the range holds %d items and %d predicates, want both releases'", len(in.ItemIDs), len(in.Predicates))
	}

	// A service with no release at all has nothing in force: every consumer
	// contract it derived belongs to a candidate that has not merged.
	empty, err := g.check.ConsumerContractsInForce(ctx, g.producer.ID)
	if err != nil {
		t.Fatalf("ConsumerContractsInForce on a service with no release: %v", err)
	}
	if empty.HighestNumber != 0 || len(empty.Predicates) != 0 {
		t.Fatalf("a service with no release has %+v in force", empty)
	}
}
