// The grouping an owner declares at Factory: one area, one nested inside
// another, and what the call refuses.
package main

import (
	"net/http"
	"testing"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/screens"
)

// TestAnAreaIsDeclaredAndCanLieInsideAnother: an owner declares the groupings
// the rest of the factory is scoped against, and the outer one is named by the
// id the declaration before it answered with.
func TestAnAreaIsDeclaredAndCanLieInsideAnother(t *testing.T) {
	ctx, d, out := newPath(t, approvals)
	s := newScreens(t, ctx, d, out)

	outerID := s.mustCall(t, "declareArea", screens.DeclareAreaArgs{Name: "billing"})
	if outerID == "" {
		t.Fatal("declareArea answered with no id")
	}
	if id := s.mustCall(t, "declareArea", screens.DeclareAreaArgs{
		Name: "billing/refunds", InsideAreaID: outerID,
	}); id == "" {
		t.Fatal("declareArea inside another answered with no id")
	}

	inner, found, err := area.ByName(ctx, d.pool, "billing/refunds")
	if err != nil || !found {
		t.Fatalf("ByName = %+v, %v, %v", inner, found, err)
	}
	chain, _, err := area.Chain(ctx, d.pool, inner.ID)
	if err != nil {
		t.Fatalf("Chain: %v", err)
	}
	if len(chain) != 2 || chain[1].Name != "billing" {
		t.Errorf("the chain is %d areas ending at %q, want two ending at billing", len(chain), chain[len(chain)-1].Name)
	}
	if chain[0].Actor.Kind != record.KindHuman {
		t.Errorf("the area's actor is %+v, want the owner who declared it", chain[0].Actor)
	}

	// Factory lists every area declared, not the one the composition names:
	// each with what it lies inside and the project its chain ends at, so a
	// nested area is readable without walking the chain at the screen.
	var factory screens.Factory
	s.get(t, "/api/factory", &factory)
	listed := map[string]screens.Area{}
	for _, one := range factory.Areas {
		listed[one.Name] = one
	}
	for _, name := range []string{theArea, "billing", "billing/refunds"} {
		if _, found := listed[name]; !found {
			t.Errorf("Factory lists no area named %q: %+v", name, factory.Areas)
		}
	}
	if listed["billing/refunds"].Inside != outerID {
		t.Errorf("the nested area reads inside %q, want the area it was declared in, %s",
			listed["billing/refunds"].Inside, outerID)
	}
	if listed["billing/refunds"].ProjectID != s.p.projectID {
		t.Errorf("the nested area reads project %q, want the one its chain ends at, %s",
			listed["billing/refunds"].ProjectID, s.p.projectID)
	}

	// An area inside one nobody declared is refused as an address that resolves
	// to nothing, and an area with no name is refused by its own writer.
	if status, body := s.call(t, "declareArea", screens.DeclareAreaArgs{
		Name: "marketing", InsideAreaID: "ar_nothing",
	}); status != http.StatusNotFound {
		t.Errorf("an area inside one nobody declared answered %d: %s", status, body)
	}
	if status, body := s.call(t, "declareArea", screens.DeclareAreaArgs{}); status == http.StatusOK ||
		status == http.StatusNoContent {
		t.Errorf("an area with no name was accepted: %s", body)
	}

	// Every declaration appends a policy version, People being the one writer
	// at Factory that does not: the area goes through package policy's own
	// Factory and not beside it.
	if len(versionsOf(t, ctx, d)) < 2 {
		t.Error("the two areas appended fewer than two policy versions")
	}
}
