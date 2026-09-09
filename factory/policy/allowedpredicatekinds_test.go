package policy_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/safeguard"
)

// TestTheAllowedKindsAreTheOneListAndASafeguardMayOnlyExtendIt: the score
// supplies no list, so an unauthored one is the kinds the factory itself can
// decide rather than empty — gate policy has an owner extend the list, which
// presupposes something to extend — and both an authored value and a safeguard
// are a union over it.
func TestTheAllowedKindsAreTheOneListAndASafeguardMayOnlyExtendIt(t *testing.T) {
	ctx, in := newFactory(t)

	all, err := in.reader.All(ctx, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	allowed := effectiveOf(t, all, gatepolicy.AllowedPredicateKinds)
	own := gatepolicy.AllowedPredicateKindNames()
	slices.Sort(own)
	if allowed.Source != policy.FromFactory || !slices.Equal(allowed.List, own) {
		t.Errorf("an unauthored allowed reads %v from %s, want the factory's own %v",
			allowed.List, allowed.Source, own)
	}

	authored := []string{string(gatepolicy.PredicateRange), string(gatepolicy.PredicateUnit)}
	if _, err := in.factory.AuthorAllowedPredicateKinds(ctx, owner, authored); err != nil {
		t.Fatalf("AuthorAllowedPredicateKinds: %v", err)
	}
	if _, _, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.AllowedPredicateKinds,
		safeguard.Subject{Kind: safeguard.SubjectPredicateKindsList, ID: in.settings.ID},
		safeguard.Bound{List: []string{string(gatepolicy.PredicateSentRange)}}, safeguard.Routing{}); err != nil {
		t.Fatalf("AddSafeguard: %v", err)
	}

	all, err = in.reader.All(ctx, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	allowed = effectiveOf(t, all, gatepolicy.AllowedPredicateKinds)
	if !slices.Equal(allowed.List, own) {
		t.Errorf("the allowed reads %v, want the union %v", allowed.List, own)
	}
	if allowed.Source != policy.FromAuthored {
		t.Errorf("the allowed reads from %s, want the authored value", allowed.Source)
	}
}

// TestAKindNothingCanDecideNeverReachesTheList is
// ../../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md's
// "a predicate decidable against one observed exchange, which is a floor no
// safeguard goes below": the floor is held where the list is widened and not
// where a consumer contract is derived, so a name this factory has no decider
// for is refused at the write — by an owner authoring it and by a safeguard
// adding it alike — and the list in force never holds one.
func TestAKindNothingCanDecideNeverReachesTheList(t *testing.T) {
	ctx, in := newFactory(t)

	_, err := in.factory.AuthorAllowedPredicateKinds(ctx, owner,
		[]string{string(gatepolicy.PredicateRead), "schema"})
	if !errors.Is(err, gatepolicy.ErrPredicateKindUnknown) {
		t.Errorf("authoring a kind nothing decides = %v, want ErrPredicateKindUnknown", err)
	}
	_, _, err = in.factory.AddSafeguard(ctx, owner, gatepolicy.AllowedPredicateKinds,
		safeguard.Subject{Kind: safeguard.SubjectPredicateKindsList, ID: in.settings.ID},
		safeguard.Bound{List: []string{"schema"}}, safeguard.Routing{})
	if !errors.Is(err, gatepolicy.ErrPredicateKindUnknown) {
		t.Errorf("a safeguard adding a kind nothing decides = %v, want ErrPredicateKindUnknown", err)
	}

	all, err := in.reader.All(ctx, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	allowed := effectiveOf(t, all, gatepolicy.AllowedPredicateKinds)
	if slices.Contains(allowed.List, "schema") {
		t.Errorf("the list in force holds a kind nothing can decide: %v", allowed.List)
	}
}
