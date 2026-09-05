// Tests of the safeguard subcommand: a safeguard placed on a subject
// written kind:name, and withdrawn by id.
package main

import (
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/safeguard"
	"github.com/dulguun0225/borg/factory/score"
)

// TestASafeguardIsPlacedOnASubjectByNameAndWithdrawnById: the direction is never
// typed, the subject is written kind:name, and withdrawing is what stops a
// mechanism reading it.
func TestASafeguardIsPlacedOnASubjectByNameAndWithdrawnById(t *testing.T) {
	ctx, pool := newOwner(t)
	install(t, ctx, pool)
	decomposeService(t, ctx, pool, "checkout")
	if err := areaCommand([]string{"payments"}); err != nil {
		t.Fatalf("area: %v", err)
	}

	for _, c := range []struct {
		args      []string
		parameter gatepolicy.Parameter
		direction gatepolicy.Direction
	}{
		{[]string{"-parameter", "risk_threshold", "-subject", "gate_row:deploy_to_production"},
			gatepolicy.RiskThreshold, gatepolicy.DirectionAddsAHuman},
		{[]string{"-parameter", "window_limit", "-subject", "service:checkout", "-bound", "2"},
			gatepolicy.WindowLimit, gatepolicy.DirectionCeiling},
		{[]string{"-parameter", "item_size_target", "-subject", "area:payments", "-bound", "300"},
			gatepolicy.ItemSizeTarget, gatepolicy.DirectionCeiling},
		{[]string{"-parameter", "allowed_predicate_kinds", "-subject", "factory_settings:", "-bound", "status,schema"},
			gatepolicy.AllowedPredicateKinds, gatepolicy.DirectionFloor},
	} {
		if err := safeguardCommand(c.args); err != nil {
			t.Fatalf("safeguard %v: %v", c.args, err)
		}
	}

	safeguards, err := safeguard.All(ctx, pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(safeguards) != 4 {
		t.Fatalf("%d safeguards are placed, want four", len(safeguards))
	}
	for _, p := range safeguards {
		if p.Withdrawn {
			t.Errorf("safeguard %s is withdrawn the moment it was placed", p.ID)
		}
		if p.Subject.Kind == safeguard.SubjectService && !strings.HasPrefix(p.Subject.ID, "svc_") {
			t.Errorf("the safeguard on a service names %q, want the record's id", p.Subject.ID)
		}
		if p.Subject.Kind == safeguard.SubjectArea && !strings.HasPrefix(p.Subject.ID, "ar_") {
			t.Errorf("the safeguard on an area names %q, want the record's id", p.Subject.ID)
		}
	}

	// A safeguard on the factory-wide settings record names the record's id, because
	// that is what the mechanism reading safeguards on it reads them by — a safeguard
	// naming the word would apply to nothing.
	fp, err := factorysettings.Get(ctx, pool)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	onTheRecord := 0
	for _, p := range safeguards {
		if p.Subject.Kind == safeguard.SubjectFactorySettings {
			onTheRecord++
			if p.Subject.ID != fp.ID {
				t.Errorf("the safeguard on the factory-wide settings record names %q, want %s", p.Subject.ID, fp.ID)
			}
		}
	}
	if onTheRecord != 1 {
		t.Errorf("%d safeguards name the factory-wide settings record, want the one", onTheRecord)
	}

	// The safeguard on the allowed predicate kinds reaches the parameter it was
	// drawn on: what an owner reads afterwards is the union, which is the whole of
	// what a safeguard on a list does.
	allowed, err := policy.NewReader(pool, score.Version{}).All(ctx, policy.Subjects{
		GateRow: "merge_to_master", Stage: item.StageImplementation,
	})
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	for _, e := range allowed {
		if e.Parameter != gatepolicy.AllowedPredicateKinds {
			continue
		}
		// The factory's own kinds are the floor an owner extends, so the safeguard's
		// two names are added to them rather than replacing them.
		want := len(gatepolicy.PredicateKinds) + 2
		if len(e.List) != want || !e.Clamped {
			t.Errorf("the allowed reads %v clamped %v, want the factory's own %d plus the two the safeguard added",
				e.List, e.Clamped, len(gatepolicy.PredicateKinds))
		}
	}

	if err := safeguardCommand([]string{"-withdraw", safeguards[0].ID}); err != nil {
		t.Fatalf("safeguard -withdraw: %v", err)
	}
	safeguards, err = safeguard.All(ctx, pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	withdrawn := 0
	for _, p := range safeguards {
		if p.Withdrawn {
			withdrawn++
		}
	}
	if withdrawn != 1 {
		t.Errorf("%d safeguards are withdrawn, want the one", withdrawn)
	}
}

// TestASafeguardRefusesWhatItCannotBind: a subject kind this milestone has no
// record for, a subject that is not written kind:name, a bound of the wrong
// shape, and a gate row that is not one of the rows built.
func TestASafeguardRefusesWhatItCannotBind(t *testing.T) {
	ctx, pool := newOwner(t)
	install(t, ctx, pool)

	for _, c := range []struct {
		name string
		args []string
	}{
		{"nothing at all", nil},
		{"a project", []string{"-parameter", "window_limit", "-subject", "project:payments", "-bound", "2"}},
		{"a subject with no kind", []string{"-parameter", "window_limit", "-subject", "checkout", "-bound", "2"}},
		{"a gate row nobody built", []string{"-parameter", "risk_threshold", "-subject", "gate_row:deploy_to_staging"}},
		{"a word where a bound belongs", []string{"-parameter", "window_limit", "-subject", "factory_settings:", "-bound", "two"}},
		{"a parameter that does not exist", []string{"-parameter", "gut_feel", "-subject", "factory_settings:", "-bound", "2"}},
		{"a safeguard withdrawn that does not exist", []string{"-withdraw", "sfg_nothing"}},
	} {
		if err := safeguardCommand(c.args); err == nil {
			t.Errorf("safeguard with %s was accepted", c.name)
		}
	}

	placed, err := safeguard.All(ctx, pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(placed) != 0 {
		t.Errorf("%d safeguards were placed by refused calls", len(placed))
	}
}
