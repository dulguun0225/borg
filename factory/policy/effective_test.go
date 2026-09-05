package policy_test

import (
	"slices"
	"testing"

	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/safeguard"
)

// TestTheValueInForceIsAReadOfThreeThings: what an owner authored, what the score
// supplies where they authored nothing, and the clamp a safeguard applies.
func TestTheValueInForceIsAReadOfThreeThings(t *testing.T) {
	ctx, in := newFactory(t)

	// Nothing authored: the score supplies, and a factory with nothing authored
	// in it runs.
	all, err := in.reader.All(ctx, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	limit := effectiveOf(t, all, gatepolicy.WindowLimit)
	supplied := startingValue(t, gatepolicy.WindowLimit)
	if limit.Source != policy.FromSupplied || limit.Number != supplied {
		t.Errorf("the window limit with nothing authored reads %v from %s, want the supplied %v", limit.Number, limit.Source, supplied)
	}

	// Authored: the owner's value stands, and the score's does not.
	if _, err := in.factory.AuthorWindowLimit(ctx, owner, in.service.ID, 4); err != nil {
		t.Fatalf("AuthorWindowLimit: %v", err)
	}
	all, err = in.reader.All(ctx, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if limit = effectiveOf(t, all, gatepolicy.WindowLimit); limit.Source != policy.FromAuthored || limit.Number != 4 {
		t.Errorf("the window limit reads %v from %s, want the authored 4", limit.Number, limit.Source)
	}

	// A safeguard: a ceiling over the window limit caps the authored value, and the safeguard
	// that did it is named.
	placed, _, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.WindowLimit,
		safeguard.Subject{Kind: safeguard.SubjectService, ID: in.service.ID}, safeguard.Bound{Number: 2})
	if err != nil {
		t.Fatalf("AddSafeguard: %v", err)
	}
	all, err = in.reader.All(ctx, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	limit = effectiveOf(t, all, gatepolicy.WindowLimit)
	if limit.Number != 2 || !limit.Clamped {
		t.Errorf("the window limit reads %v clamped %v, want the safeguard's ceiling of 2", limit.Number, limit.Clamped)
	}
	if !slices.Contains(limit.Safeguards, placed.ID) {
		t.Errorf("the window limit names safeguards %v, want the one placed", limit.Safeguards)
	}
	if limit.Source != policy.FromAuthored {
		t.Errorf("the window limit says its value came from %s, and a safeguard is a bound rather than a source", limit.Source)
	}
}

// TestEveryParameterResolvesAndFiveAreReadByNothing: an owner can author every
// parameter this milestone gives a writer, and the read says which of the
// thirteen changes anything at this milestone rather than leaving an owner to
// discover it.
func TestEveryParameterResolvesAndFiveAreReadByNothing(t *testing.T) {
	ctx, in := newFactory(t)

	authorings := []struct {
		parameter gatepolicy.Parameter
		author    func() (policy.Version, error)
		want      float64
	}{
		{gatepolicy.RiskThreshold, func() (policy.Version, error) {
			return in.factory.AuthorGateThreshold(ctx, owner, in.prod.ID, "merge_to_master", 0.5)
		}, 0.5},
		{gatepolicy.AttemptLimit, func() (policy.Version, error) {
			return in.factory.AuthorAttemptLimit(ctx, owner, item.StageImplementation, 5)
		}, 5},
		{gatepolicy.ItemSizeTarget, func() (policy.Version, error) {
			return in.factory.AuthorItemSizeTarget(ctx, owner, in.area.ID, 400)
		}, 400},
		{gatepolicy.WindowSize, func() (policy.Version, error) {
			return in.factory.AuthorWindowSize(ctx, owner, in.service.ID, gatepolicy.QuantityErrorRate, 0.01)
		}, 0.01},
		{gatepolicy.WindowConfidence, func() (policy.Version, error) {
			return in.factory.AuthorWindowConfidence(ctx, owner, in.service.ID, 0.98)
		}, 0.98},
		{gatepolicy.WindowCap, func() (policy.Version, error) {
			return in.factory.AuthorWindowCap(ctx, owner, in.service.ID, 3600)
		}, 3600},
		{gatepolicy.WindowLimit, func() (policy.Version, error) {
			return in.factory.AuthorWindowLimit(ctx, owner, in.service.ID, 3)
		}, 3},
	}
	for _, a := range authorings {
		version, err := a.author()
		if err != nil {
			t.Fatalf("authoring %s: %v", a.parameter, err)
		}
		if version.Parameter != a.parameter {
			t.Errorf("authoring %s appended a version naming %s", a.parameter, version.Parameter)
		}
	}
	if _, err := in.factory.AuthorAllowedPredicateKinds(ctx, owner, []string{"status"}); err != nil {
		t.Fatalf("AuthorAllowedPredicateKinds: %v", err)
	}

	all, err := in.reader.All(ctx, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != len(gatepolicy.Definitions) {
		t.Fatalf("%d parameters resolved, want %d", len(all), len(gatepolicy.Definitions))
	}
	for _, a := range authorings {
		e := effectiveOf(t, all, a.parameter)
		if e.Source != policy.FromAuthored || e.Number != a.want {
			t.Errorf("%s reads %v from %s, want the authored %v", a.parameter, e.Number, e.Source, a.want)
		}
	}

	read := 0
	for _, e := range all {
		if e.ReadBy != "" {
			read++
		}
	}
	// Eight of the thirteen are read by something now that contracts are built:
	// the threshold, the limit, the window's size, confidence, cap and limit, the
	// held-out sample rate, and the list of allowed predicate kinds, whose reader
	// is the derivation of a consumer contract. The five left are the exposure
	// bound, the advisory severity, the item-size target, the window's power, and
	// the review sample rate, none of which anything reads yet.
	if read != 8 {
		t.Errorf("%d parameters are read by something at this milestone, want eight", read)
	}
	unreadWant := []gatepolicy.Parameter{
		gatepolicy.ExposureBound, gatepolicy.AdvisorySeverity, gatepolicy.ItemSizeTarget,
		gatepolicy.WindowPower, gatepolicy.ReviewSampleRate,
	}
	for _, unread := range unreadWant {
		if e := effectiveOf(t, all, unread); e.ReadBy != "" {
			t.Errorf("%s says it is read by %q, and nothing reads it yet", unread, e.ReadBy)
		}
	}

	// The role-prompt-or-skill threshold is the same parameter on the factory-wide
	// settings record, which is where the row that decides what an agent is told reads it.
	if _, err := in.factory.AuthorRolePromptOrSkillThreshold(ctx, owner, 0.15); err != nil {
		t.Fatalf("AuthorRolePromptOrSkillThreshold: %v", err)
	}
	stored, err := factorysettings.Get(ctx, in.pool)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !stored.RolePromptOrSkillThreshold.Present || stored.RolePromptOrSkillThreshold.Number != 0.15 {
		t.Errorf("the role-prompt-or-skill threshold reads back as %+v", stored.RolePromptOrSkillThreshold)
	}
}
