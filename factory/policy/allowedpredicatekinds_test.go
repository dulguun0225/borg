package policy_test

import (
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

	if _, err := in.factory.AuthorAllowedPredicateKinds(ctx, owner, []string{"status", "field-present"}); err != nil {
		t.Fatalf("AuthorAllowedPredicateKinds: %v", err)
	}
	if _, _, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.AllowedPredicateKinds,
		safeguard.Subject{Kind: safeguard.SubjectPredicateKindsList, ID: in.settings.ID}, safeguard.Bound{List: []string{"schema", "status"}}, safeguard.Routing{}); err != nil {
		t.Fatalf("AddSafeguard: %v", err)
	}

	all, err = in.reader.All(ctx, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	allowed = effectiveOf(t, all, gatepolicy.AllowedPredicateKinds)
	want := append([]string{"field-present", "schema", "status"}, own...)
	slices.Sort(want)
	if !slices.Equal(allowed.List, want) {
		t.Errorf("the allowed reads %v, want the union %v", allowed.List, want)
	}
	if !allowed.Clamped || allowed.Source != policy.FromAuthored {
		t.Errorf("the allowed reads clamped %v from %s", allowed.Clamped, allowed.Source)
	}
}
