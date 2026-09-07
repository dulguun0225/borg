// Episode one of the demonstration, over HTTP: the install with nothing in it,
// the readiness reading it is met with, the owner's first act at Factory, and
// the first intent and the first constraint.
package main

import (
	"net/http"
	"testing"

	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/fleetentry"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/screens"
)

// TestEpisodeOneIsAnInstallWithNothingInIt is the first episode: serve against
// a fresh store, the home view showing every role uncovered in the readiness
// reading and the badge counting it, one fleet entry per role written at
// Factory emptying the reading, and then duty 1 and duty 2 — the first intent
// taken in at Work and a constraint supplied at Factory with the reach it
// binds.
//
// The composition writes no entry of its own, which is what
// [deps.withoutFleetEntries] is for: the terminal's stand-in for the owner's
// first act at Factory would hide the state this episode is about.
func TestEpisodeOneIsAnInstallWithNothingInIt(t *testing.T) {
	ctx, d, out := newPath(t, approvals)
	d.withoutFleetEntries = true
	s := newScreens(t, ctx, d, out)

	var home screens.Home
	s.get(t, "/api/home", &home)
	if len(home.Readiness) != len(dispatch.Roles) {
		t.Fatalf("the readiness reading has %d row(s), want one per role: %+v", len(home.Readiness), home.Readiness)
	}
	for _, row := range home.Readiness {
		if row.EntryCovers {
			t.Errorf("role %s reads as covered on an install holding no fleet entry", row.Role)
		}
	}
	if home.Badge.FactoryHoldsForAHuman != int64(len(dispatch.Roles)) {
		t.Errorf("the badge counts %d of the factory's own holds, want one per uncovered role, %d",
			home.Badge.FactoryHoldsForAHuman, len(dispatch.Roles))
	}
	if home.Badge.Total != home.Badge.FactoryHoldsForAHuman {
		t.Errorf("the badge totals %d with %d uncovered role(s) and nothing else waiting",
			home.Badge.Total, home.Badge.FactoryHoldsForAHuman)
	}
	if home.Digest != nil {
		t.Error("the digest is shown with the badge above zero, and it is the part that appears only at zero")
	}

	// The owner's first act at Factory: one entry per role, and the reading
	// emptying as each is written.
	for n, role := range dispatch.Roles {
		if s.mustCall(t, "writeFleetEntry", theFleetEntry(string(role))) == "" {
			t.Fatalf("writeFleetEntry for %s answered with no id", role)
		}
		s.get(t, "/api/home", &home)
		want := int64(len(dispatch.Roles) - n - 1)
		if home.Badge.FactoryHoldsForAHuman != want {
			t.Errorf("after %d entry(s) the badge counts %d uncovered role(s), want %d",
				n+1, home.Badge.FactoryHoldsForAHuman, want)
		}
	}
	covered, err := fleetentry.CoveredRoles(ctx, d.pool)
	if err != nil {
		t.Fatalf("reading the covered roles: %v", err)
	}
	for _, role := range dispatch.Roles {
		if !covered[string(role)] {
			t.Errorf("role %s is uncovered after an entry was written for it", role)
		}
	}

	// Duty 1 at Work, and duty 2 at Factory beside it.
	intentID := s.mustCall(t, "supplyIntent", screens.SupplyIntentArgs{
		Statement: theStatement, Services: []string{theService},
	})
	if intentID == "" {
		t.Fatal("supplyIntent answered with no id")
	}
	constraintID := s.mustCall(t, "supplyConstraint", screens.SupplyConstraintArgs{
		Statement: "Every service shall keep an audit trail of every charge.",
		ReachKind: "factory",
	})
	if constraintID == "" {
		t.Fatal("supplyConstraint answered with no id")
	}

	var factory screens.Factory
	s.get(t, "/api/factory", &factory)
	if len(factory.FleetEntries) != len(dispatch.Roles) {
		t.Errorf("Factory holds %d fleet entry(s), want one per role", len(factory.FleetEntries))
	}
	found := false
	for _, one := range factory.Constraints {
		if one.ID == constraintID && one.Reach == "factory" {
			found = true
		}
	}
	if !found {
		t.Errorf("Factory does not list the constraint it was supplied with, by reach: %+v", factory.Constraints)
	}

	var constraint screens.Constraint
	s.get(t, "/api/constraint/"+constraintID, &constraint)
	if constraint.Reach != "factory" || constraint.Statement == "" {
		t.Errorf("the constraint's own address reads %+v, want the factory reach and its statement", constraint)
	}

	// The intent is in Work before decomposition has written an item for it,
	// which is what the intent stage of the pass is read from.
	var work screens.Work
	s.get(t, "/api/work", &work)
	_ = work
	if s.status(t, "/api/item/it_nothing") != http.StatusNotFound {
		t.Error("an id naming no item was answered rather than refused with a 404")
	}
}

// TestAReadOnlyPeopleRowReadsAndActsNowhere is the row the design adds only so
// a human can read the four screens: every acting call refuses it, and its
// reads are answered.
func TestAReadOnlyPeopleRowReadsAndActsNowhere(t *testing.T) {
	ctx, d, out := newPath(t, approvals)
	d.withoutFleetEntries = true
	s := newScreens(t, ctx, d, out)

	// A key with a mapping and nothing else: no duty, no obligation, and no
	// credential lent.
	reader := owner(t, ctx, d.pool, d.token, "reader")
	s.principal = reader.Key

	status, body := s.call(t, "writeFleetEntry", theFleetEntry(string(dispatch.Roles[0])))
	if status == http.StatusNoContent || status == http.StatusOK {
		t.Fatalf("a read-only row wrote a fleet entry: %s", body)
	}
	status, body = s.call(t, "supplyIntent", screens.SupplyIntentArgs{
		Statement: theStatement, Services: []string{theService},
	})
	if status == http.StatusNoContent || status == http.StatusOK {
		t.Fatalf("a read-only row took an intent in: %s", body)
	}

	// The reads are answered: the row reads all four screens.
	for _, address := range []string{"/api/home", "/api/work", "/api/ops", "/api/factory", "/api/people"} {
		if code := s.status(t, address); code != http.StatusOK {
			t.Errorf("GET %s as a read-only row answered %d, want 200", address, code)
		}
	}

	// Declaring one duty on that key is what makes it act, which is the one
	// reading every acting call makes before any screen-specific check.
	if err := s.made.DeclareDuty(ctx,
		principal.OfHuman(s.p.human.Key, record.BasisClaimed),
		screens.DeclareDutyArgs{HumanKey: reader.Key, Duty: 1}); err != nil {
		t.Fatalf("declaring a duty on the row: %v\n%s", err, out)
	}
	if id := s.mustCall(t, "supplyIntent", screens.SupplyIntentArgs{
		Statement: theStatement, Services: []string{theService},
	}); id == "" {
		t.Error("supplyIntent answered with no id after the row was given a duty")
	}
}
