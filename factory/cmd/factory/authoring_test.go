// Tests of the author subcommand: which parameter reads which subject flag,
// and what it refuses to resolve.
package main

import (
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/service"
)

// TestNothingToAuthorOnBeforeTheFactoryIsInstalled: the two records an owner
// authors on are created by the run's first take, and an error naming a missing
// version says that badly on its own — so the subcommand says what to do.
func TestNothingToAuthorOnBeforeTheFactoryIsInstalled(t *testing.T) {
	_, _ = newOwner(t)

	for _, c := range []struct {
		name string
		run  func() error
	}{
		{"policy", func() error { return policyCommand(nil) }},
		{"author", func() error {
			return authorCommand([]string{"-parameter", "attempt_limit", "-value", "5"})
		}},
	} {
		err := c.run()
		if err == nil {
			t.Errorf("%s on a factory nobody installed was accepted", c.name)
			continue
		}
		if !strings.Contains(err.Error(), "the factory is not installed") {
			t.Errorf("%s says %q, and what an owner needs to know is that nothing is installed", c.name, err)
		}
	}
}

// TestEachParameterReadsTheSubjectItsScopeNames: the record a parameter is a
// field of is a fact of the parameter and not a choice, so the subcommand reads
// the flag that parameter needs and refuses where the subject is missing.
func TestEachParameterReadsTheSubjectItsScopeNames(t *testing.T) {
	ctx, pool := newOwner(t)
	production := install(t, ctx, pool)
	svc := decomposeService(t, ctx, pool, "checkout")
	if err := areaCommand([]string{"payments"}); err != nil {
		t.Fatalf("area: %v", err)
	}
	ar, _, err := area.ByName(ctx, pool, "payments")
	if err != nil {
		t.Fatalf("ByName: %v", err)
	}

	for _, c := range []struct {
		args []string
		want float64
		read func() (float64, bool)
	}{
		{
			[]string{"-parameter", "risk_threshold", "-value", "0.2", "-gate", "merge_to_master"}, 0.2,
			func() (float64, bool) {
				authored, err := environment.GateThreshold(ctx, pool, production.ID, "merge_to_master")
				if err != nil {
					t.Fatalf("GateThreshold: %v", err)
				}
				return authored.Number, authored.Present
			},
		},
		{
			[]string{"-parameter", "attempt_limit", "-value", "5", "-stage", "implementation"}, 5,
			func() (float64, bool) {
				fp, err := factorysettings.Get(ctx, pool)
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				subject, err := factorysettings.OfStage(item.StageImplementation)
				if err != nil {
					t.Fatalf("OfStage: %v", err)
				}
				authored, err := factorysettings.AttemptLimit(ctx, pool, fp.ID, subject)
				if err != nil {
					t.Fatalf("AttemptLimit: %v", err)
				}
				return authored.Number, authored.Present
			},
		},
		{
			[]string{"-parameter", "item_size_target", "-value", "400", "-area", "payments"}, 400,
			func() (float64, bool) {
				read, err := area.Get(ctx, pool, ar.ID)
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				return read.ItemSizeTarget.Number, read.ItemSizeTarget.Present
			},
		},
		{
			[]string{"-parameter", "window_limit", "-value", "2", "-service", "checkout"}, 2,
			func() (float64, bool) {
				read, err := service.Get(ctx, pool, svc.ID)
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				return read.Parameters.WindowLimit.Number, read.Parameters.WindowLimit.Present
			},
		},
		{
			[]string{"-parameter", "window_confidence", "-value", "0.99", "-service", "checkout"}, 0.99,
			func() (float64, bool) {
				read, err := service.Get(ctx, pool, svc.ID)
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				return read.Parameters.WindowConfidence.Number, read.Parameters.WindowConfidence.Present
			},
		},
		{
			[]string{"-parameter", "risk_threshold", "-value", "0.15", "-gate", "role_prompt_or_skill"}, 0.15,
			func() (float64, bool) {
				fp, err := factorysettings.Get(ctx, pool)
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				return fp.RolePromptOrSkillThreshold.Number, fp.RolePromptOrSkillThreshold.Present
			},
		},
	} {
		if err := authorCommand(c.args); err != nil {
			t.Errorf("author %v: %v", c.args, err)
			continue
		}
		value, present := c.read()
		if !present {
			t.Errorf("author %v left nothing authored", c.args)
		}
		if value != c.want {
			t.Errorf("author %v stored %v, want %v", c.args, value, c.want)
		}
	}

	// The allowed predicate kinds are the one list, and they are authored as one.
	if err := authorCommand([]string{"-parameter", "allowed_predicate_kinds", "-value", "status,schema"}); err != nil {
		t.Fatalf("author the allowed: %v", err)
	}
	fp, err := factorysettings.Get(ctx, pool)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(fp.AllowedPredicateKinds) != 2 {
		t.Errorf("the allowed reads %v, want the two authored", fp.AllowedPredicateKinds)
	}

	// Every authoring write appended a policy version, so the sequence is as long
	// as the writes plus the three the install made — the factory-wide settings
	// record, the project, and production's environment for it.
	versions, err := policy.All(ctx, pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(versions) != 10 {
		t.Errorf("%d policy versions exist, want three creations and seven authorings", len(versions))
	}
}

// TestAuthoringRefusesWhatItCannotResolve: a parameter that is not one of the
// eight, a value of the wrong shape, and a subject the parameter needs and the
// owner did not give.
func TestAuthoringRefusesWhatItCannotResolve(t *testing.T) {
	ctx, pool := newOwner(t)
	install(t, ctx, pool)

	for _, c := range []struct {
		name string
		args []string
	}{
		{"no parameter", []string{"-value", "2"}},
		{"no value", []string{"-parameter", "k"}},
		{"a parameter that does not exist", []string{"-parameter", "gut_feel", "-value", "2"}},
		{"a word where a number belongs", []string{"-parameter", "window_limit", "-value", "two", "-service", "checkout"}},
		{"no service for a service-scoped parameter", []string{"-parameter", "window_limit", "-value", "2"}},
		{"no area for an area-scoped parameter", []string{"-parameter", "item_size_target", "-value", "400"}},
		{"an area nobody declared", []string{"-parameter", "item_size_target", "-value", "400", "-area", "nothing"}},
		{"a service nobody decomposed", []string{"-parameter", "window_limit", "-value", "2", "-service", "nothing"}},
	} {
		if err := authorCommand(c.args); err == nil {
			t.Errorf("author with %s was accepted", c.name)
		}
	}

	// Nothing was authored, so nothing moved the policy version past the
	// install's three creations.
	versions, err := policy.All(ctx, pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(versions) != 3 {
		t.Errorf("%d policy versions exist after refused writes, want the install's three", len(versions))
	}
}
