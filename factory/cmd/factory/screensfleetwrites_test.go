// Factory's writes that dispatch re-matches on, over HTTP: a fleet entry whose
// scope names records by name, and the seam 5 field a document-kind constraint
// waits for.
package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/constraint"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/screens"
)

// TestAFleetEntrysScopeIsResolvedByName: the scope an owner types at Factory is
// a project, a service and an area by name, resolved to the ids the record
// stores. A name that resolves to nothing is refused rather than stored — an
// entry scoped to a record that does not exist matches no item, and the fleet
// not growing itself is already what an unmatched stage costs.
func TestAFleetEntrysScopeIsResolvedByName(t *testing.T) {
	ctx, d, out := newPath(t, approvals)
	s := newScreens(t, ctx, d, out)

	for _, refused := range []screens.WriteFleetEntryArgs{
		{ScopeProjectName: "no such project"},
		{ScopeServiceName: "no such service"},
		{ScopeAreaName: "no such area"},
	} {
		entry := theFleetEntry(string(dispatch.Roles[0]))
		entry.ScopeProjectName, entry.ScopeServiceName, entry.ScopeAreaName =
			refused.ScopeProjectName, refused.ScopeServiceName, refused.ScopeAreaName
		status, body := s.call(t, "writeFleetEntry", entry)
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("an entry scoped to %+v answered %d: %s, want the write refused", refused, status, body)
		}
	}

	scoped := theFleetEntry(string(dispatch.Roles[0]))
	scoped.ScopeProjectName, scoped.ScopeServiceName, scoped.ScopeAreaName =
		d.project, theService, theArea
	id := s.mustCall(t, "writeFleetEntry", scoped)

	var factory screens.Factory
	s.get(t, "/api/factory", &factory)
	var written screens.FleetEntry
	for _, one := range factory.FleetEntries {
		if one.ID == id {
			written = one
		}
	}
	if written.ScopeProjectID != s.p.projectID || written.ScopeServiceID == "" || written.ScopeAreaID == "" {
		t.Errorf("the entry's scope is %+v, want the ids the three names resolved to", written)
	}
	if written.ScopeProjectID == d.project || written.ScopeAreaID == theArea {
		t.Errorf("the entry's scope is %+v, want ids and not the names they were written from", written)
	}
}

// TestSeam5IsRequiredOnAConstraintAndTurnedOnAtFactory: a document-kind
// constraint may require seam 5 enforced, and an item within its reach waits at
// dispatch until the factory-wide settings record says it is. Both halves are
// reachable from Factory — the requirement on the constraint an owner supplies,
// and the field itself. What the hold does when the field turns on is package
// dispatch's own re-match, tested there; this is that both ends of it can be
// written from a screen at all.
func TestSeam5IsRequiredOnAConstraintAndTurnedOnAtFactory(t *testing.T) {
	ctx, d, out := newPath(t, approvals)
	s := newScreens(t, ctx, d, out)

	var factory screens.Factory
	s.get(t, "/api/factory", &factory)
	if factory.Seam5Enforced {
		t.Fatal("a fresh install enforces seam 5, and the field is off until an owner turns it on")
	}

	id := s.mustCall(t, "supplyConstraint", screens.SupplyConstraintArgs{
		Statement:             "Every call into this factory shall carry an authenticated principal.",
		ReachKind:             "factory",
		RequiresSeam5Enforced: true,
	})
	supplied, err := constraint.Get(ctx, d.pool, id)
	if err != nil {
		t.Fatalf("Get(%s): %v", id, err)
	}
	if !supplied.RequiresSeam5Enforced {
		t.Errorf("the constraint is %+v, want the seam 5 requirement the call carried", supplied)
	}

	// The field is one-way: off at install, turned on once, never off again.
	status, body := s.call(t, "setSeam5Enforced", screens.SetSeam5EnforcedArgs{})
	if status != http.StatusUnprocessableEntity ||
		!strings.Contains(body, "turned on once and never off") {
		t.Fatalf("turning seam 5 off answered %d: %s, want the refusal", status, body)
	}

	s.mustCall(t, "setSeam5Enforced", screens.SetSeam5EnforcedArgs{Enforced: true})
	settings, err := factorysettings.Get(ctx, d.pool)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !settings.Seam5Enforced {
		t.Error("the settings record does not enforce seam 5 after the owner turned it on")
	}
	s.get(t, "/api/factory", &factory)
	if !factory.Seam5Enforced {
		t.Error("Factory reads seam 5 as unenforced after the owner turned it on")
	}
}
