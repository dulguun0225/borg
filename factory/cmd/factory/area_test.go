// Tests of the area subcommand: a grouping declared and nested inside
// another.
package main

import (
	"testing"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/record"
)

// TestAnAreaIsDeclaredAndCanLieInsideAnother: an owner declares the groupings the
// rest of the factory is scoped against, and the inside is named rather than
// given as an id.
func TestAnAreaIsDeclaredAndCanLieInsideAnother(t *testing.T) {
	ctx, pool := newOwner(t)
	install(t, ctx, pool)

	if err := areaCommand([]string{"payments"}); err != nil {
		t.Fatalf("area payments: %v", err)
	}
	if err := areaCommand([]string{"payments/refunds", "-inside", "payments"}); err != nil {
		t.Fatalf("area payments/refunds: %v", err)
	}

	inner, found, err := area.ByName(ctx, pool, "payments/refunds")
	if err != nil || !found {
		t.Fatalf("ByName = %+v, %v, %v", inner, found, err)
	}
	chain, _, err := area.Chain(ctx, pool, inner.ID)
	if err != nil {
		t.Fatalf("Chain: %v", err)
	}
	if len(chain) != 2 || chain[1].Name != "payments" {
		t.Errorf("the chain is %d areas ending at %q, want two ending at payments", len(chain), chain[len(chain)-1].Name)
	}
	if chain[0].Actor.Kind != record.KindHuman {
		t.Errorf("the area's actor is %+v, want the owner who declared it", chain[0].Actor)
	}

	if err := areaCommand([]string{"marketing", "-inside", "nothing"}); err == nil {
		t.Error("an area inside one nobody declared was accepted")
	}
	if err := areaCommand(nil); err == nil {
		t.Error("area with no name was accepted")
	}
}
