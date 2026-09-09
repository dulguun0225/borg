// What an owner authors at Factory: which parameter reads which subject, what
// the call refuses to resolve, and the policy version every write appends.
package main

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/screens"
	"github.com/dulguun0225/borg/factory/service"
)

// TestNothingToReadBeforeTheFactoryIsInstalled: the two records an owner
// authors on are created by the install, and an error naming a missing version
// says that badly on its own — so the read says what to do.
func TestNothingToReadBeforeTheFactoryIsInstalled(t *testing.T) {
	_, _ = newOwner(t)

	err := policyCommand(nil)
	if err == nil {
		t.Fatal("policy on a factory nobody installed was accepted")
	}
	if !strings.Contains(err.Error(), "the factory is not installed") {
		t.Errorf("policy says %q, and what an owner needs to know is that nothing is installed", err)
	}
}

// TestEachParameterReadsTheSubjectItsScopeNames: the record a parameter is a
// field of is a fact of the parameter and not a choice, so the call reads the
// subject that parameter names and refuses where it is missing.
func TestEachParameterReadsTheSubjectItsScopeNames(t *testing.T) {
	ctx, d, out := newPath(t, approvals)
	s := newScreens(t, ctx, d, out)
	production := s.p.production
	svc, found, err := service.ByName(ctx, d.pool, theService)
	if err != nil || !found {
		t.Fatalf("ByName(%s) = found %v, %v", theService, found, err)
	}
	if s.mustCall(t, "declareArea", screens.DeclareAreaArgs{Name: "billing"}) == "" {
		t.Fatal("declareArea answered with no id")
	}
	ar, _, err := area.ByName(ctx, d.pool, "billing")
	if err != nil {
		t.Fatalf("ByName(billing): %v", err)
	}

	before := len(versionsOf(t, ctx, d))
	authorings := []struct {
		args screens.AuthorParameterArgs
		want float64
		read func() (float64, bool)
	}{
		{
			screens.AuthorParameterArgs{Parameter: "risk_threshold", Value: "0.2", GateRow: "merge_to_master"}, 0.2,
			func() (float64, bool) {
				authored, err := environment.GateThreshold(ctx, d.pool, production.ID, "merge_to_master")
				if err != nil {
					t.Fatalf("GateThreshold: %v", err)
				}
				return authored.Number, authored.Present
			},
		},
		{
			screens.AuthorParameterArgs{Parameter: "attempt_limit", Value: "5", Stage: "implementation"}, 5,
			func() (float64, bool) {
				fp, err := factorysettings.Get(ctx, d.pool)
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				subject, err := factorysettings.OfStage(item.StageImplementation)
				if err != nil {
					t.Fatalf("OfStage: %v", err)
				}
				authored, err := factorysettings.AttemptLimit(ctx, d.pool, fp.ID, subject)
				if err != nil {
					t.Fatalf("AttemptLimit: %v", err)
				}
				return authored.Number, authored.Present
			},
		},
		{
			screens.AuthorParameterArgs{Parameter: "item_size_target", Value: "400", AreaID: "billing"}, 400,
			func() (float64, bool) {
				read, err := area.Get(ctx, d.pool, ar.ID)
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				return read.ItemSizeTarget.Number, read.ItemSizeTarget.Present
			},
		},
		{
			screens.AuthorParameterArgs{Parameter: "window_limit", Value: "2", ServiceID: theService}, 2,
			func() (float64, bool) {
				read, err := service.Get(ctx, d.pool, svc.ID)
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				return read.Parameters.WindowLimit.Number, read.Parameters.WindowLimit.Present
			},
		},
		{
			screens.AuthorParameterArgs{Parameter: "window_confidence", Value: "0.99", ServiceID: theService}, 0.99,
			func() (float64, bool) {
				read, err := service.Get(ctx, d.pool, svc.ID)
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				return read.Parameters.WindowConfidence.Number, read.Parameters.WindowConfidence.Present
			},
		},
		{
			screens.AuthorParameterArgs{Parameter: "risk_threshold", Value: "0.15", GateRow: gate.RolePromptOrSkill.String()}, 0.15,
			func() (float64, bool) {
				fp, err := factorysettings.Get(ctx, d.pool)
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				return fp.RolePromptOrSkillThreshold.Number, fp.RolePromptOrSkillThreshold.Present
			},
		},
		{
			// The allowed predicate kinds are the one list, and they are
			// authored as one. Each has to be a kind this factory can decide
			// against one observed exchange, which is the floor the list's own
			// rule sets.
			screens.AuthorParameterArgs{Parameter: "allowed_predicate_kinds", Value: "range,sent_range"}, 2,
			func() (float64, bool) {
				fp, err := factorysettings.Get(ctx, d.pool)
				if err != nil {
					t.Fatalf("Get: %v", err)
				}
				return float64(len(fp.AllowedPredicateKinds)), true
			},
		},
	}
	for _, c := range authorings {
		s.mustCall(t, "authorParameter", c.args)
		value, present := c.read()
		if !present {
			t.Errorf("authoring %+v left nothing authored", c.args)
		}
		if value != c.want {
			t.Errorf("authoring %+v stored %v, want %v", c.args, value, c.want)
		}
	}

	// Every authoring write appended a policy version, and so did the area
	// declared before them: the count is read as a difference, because the
	// install and the window's four are authored by the fixture and a run of
	// the path would author more.
	after := len(versionsOf(t, ctx, d))
	if after-before != len(authorings) {
		t.Errorf("%d policy version(s) were appended by %d authoring write(s)", after-before, len(authorings))
	}
}

// TestAuthoringRefusesWhatItCannotResolve: a parameter that is none of the
// eight, a value of the wrong shape, and a subject the parameter needs and the
// owner did not give.
func TestAuthoringRefusesWhatItCannotResolve(t *testing.T) {
	ctx, d, out := newPath(t, approvals)
	s := newScreens(t, ctx, d, out)

	before := len(versionsOf(t, ctx, d))
	for _, c := range []struct {
		name string
		args screens.AuthorParameterArgs
	}{
		{"no parameter", screens.AuthorParameterArgs{Value: "2"}},
		{"no value", screens.AuthorParameterArgs{Parameter: "window_limit"}},
		{"a parameter that does not exist", screens.AuthorParameterArgs{Parameter: "gut_feel", Value: "2"}},
		{"a word where a number belongs", screens.AuthorParameterArgs{
			Parameter: "window_limit", Value: "two", ServiceID: theService}},
		{"no service for a service-scoped parameter", screens.AuthorParameterArgs{
			Parameter: "window_limit", Value: "2"}},
		{"no area for an area-scoped parameter", screens.AuthorParameterArgs{
			Parameter: "item_size_target", Value: "400"}},
		{"an area nobody declared", screens.AuthorParameterArgs{
			Parameter: "item_size_target", Value: "400", AreaID: "nothing"}},
		{"a service nobody decomposed", screens.AuthorParameterArgs{
			Parameter: "window_limit", Value: "2", ServiceID: "nothing"}},
	} {
		if status, body := s.call(t, "authorParameter", c.args); status == http.StatusNoContent ||
			status == http.StatusOK {
			t.Errorf("authoring with %s was accepted: %s", c.name, body)
		}
	}

	// Nothing was authored, so nothing appended a policy version.
	if after := len(versionsOf(t, ctx, d)); after != before {
		t.Errorf("%d policy version(s) were appended by refused writes", after-before)
	}
}

// versionsOf is every policy version, read through [policy.Reader] with the
// lease the fixture holds — which is what makes a read after a screen's own
// write readable, the token every writer and every read event carries being
// the one this test's deps compose with.
func versionsOf(t *testing.T, ctx context.Context, d deps) []policy.Version {
	t.Helper()
	versions, err := policy.NewReader(d.pool, d.token, score.Version{}).
		Versions(ctx, asPrincipal(owner(t, ctx, d.pool, d.token, d.human)))
	if err != nil {
		t.Fatalf("reading the policy versions: %v", err)
	}
	return versions
}
