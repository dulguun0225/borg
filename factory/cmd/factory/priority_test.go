// Tests of the priority subcommand: an owner reorders a queue through
// dispatch.
package main

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/item"
)

// TestThePrioritySubcommandReordersAQueue is duty 9's other write on an item: an
// owner reorders a queue, through dispatch rather than beside it. It is the fifth
// subcommand and it has no screen until Work arrives.
func TestThePrioritySubcommandReordersAQueue(t *testing.T) {
	ctx, pool := newOwner(t)
	install(t, ctx, pool)
	svc := decomposeService(t, ctx, pool, "checkout")

	it, err := item.NewDecomposition(pool, testToken(t, ctx, pool)).Create(ctx, decompositionActor,
		item.New{IntentID: "in_a", ServiceID: svc.ID, Branch: "item/a"})
	if err != nil {
		t.Fatalf("decomposing the item: %v", err)
	}

	if err := dispatch([]string{"priority", it.ID, "-priority", "7"}); err != nil {
		t.Fatalf("priority: %v", err)
	}
	read, err := item.Get(ctx, pool, it.ID)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}
	if read.Priority != 7 {
		t.Errorf("the item's priority is %d, the owner set 7", read.Priority)
	}

	if err := dispatch([]string{"priority", "-priority", "7"}); err == nil {
		t.Error("priority with no item id was accepted")
	}
	if err := dispatch([]string{"priority", "it_missing", "-priority", "1"}); !errors.Is(err, item.ErrNotFound) {
		t.Errorf("priority on a missing item = %v, want ErrNotFound", err)
	}
}
