package policy_test

import (
	"context"
	"testing"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/policy"
)

// TestTheRateAtAThresholdIsFrozenOnTheVersionThatSetIt: the version stays in
// force while the rate moves under it, so the reference a query over the
// decisions taken under it is read against is the rate at the moment the
// threshold was set. It is per factor set, a rate pooled over the sets moving
// when the mix of gates fired moves and not only when the score did.
//
// It is also the one field a later version does not restate, so the read is a
// walk back to the version that set that threshold on that scope.
func TestTheRateAtAThresholdIsFrozenOnTheVersionThatSetIt(t *testing.T) {
	ctx, in := newFactory(t)

	rate := 0.80
	in.factory.AutoPassRates = func(context.Context, policy.Scope, string, float64) ([]policy.AutoPassRate, error) {
		return []policy.AutoPassRate{
			{FactorSet: "merge_to_master", Rate: rate},
			{FactorSet: "deploy_to_production", Rate: rate / 2},
		}, nil
	}

	set, err := in.factory.AuthorGateThreshold(ctx, owner, in.prod.ID, "merge_to_master", 0.4)
	if err != nil {
		t.Fatalf("AuthorGateThreshold: %v", err)
	}
	if len(set.AutoPassRates) != 2 || set.AutoPassRates[0].Rate != 0.80 {
		t.Fatalf("the version froze %+v", set.AutoPassRates)
	}

	// A write that touches another parameter appends a version naming the
	// threshold it did not change and no rate beside it.
	rate = 0.20
	other, err := in.factory.AuthorWindowLimit(ctx, owner, in.service.ID, 3)
	if err != nil {
		t.Fatalf("AuthorWindowLimit: %v", err)
	}
	if len(other.AutoPassRates) != 0 {
		t.Errorf("a write that set no threshold froze %+v", other.AutoPassRates)
	}

	scope := policy.Scope{Kind: policy.ScopeEnvironment, ID: in.prod.ID}
	frozen, found, err := in.reader.AuthoredAutoPassRate(ctx, ownerReading, scope, "merge_to_master")
	if err != nil {
		t.Fatalf("AuthoredAutoPassRate: %v", err)
	}
	if !found || len(frozen) != 2 || frozen[0].Rate != 0.80 {
		t.Errorf("the rate read back as %+v, found %v, want the one frozen at the setting", frozen, found)
	}

	// Another gate row on the same record has its own, and a row nothing set
	// has none.
	if _, found, err := in.reader.AuthoredAutoPassRate(ctx, ownerReading, scope, "deploy_to_production"); err != nil {
		t.Fatalf("AuthoredAutoPassRate: %v", err)
	} else if found {
		t.Error("a row no version set a threshold on has a rate")
	}

	// Setting it again freezes the rate as it stands then.
	if _, err := in.factory.AuthorGateThreshold(ctx, owner, in.prod.ID, "merge_to_master", 0.6); err != nil {
		t.Fatalf("AuthorGateThreshold: %v", err)
	}
	frozen, _, err = in.reader.AuthoredAutoPassRate(ctx, ownerReading, scope, "merge_to_master")
	if err != nil {
		t.Fatalf("AuthoredAutoPassRate: %v", err)
	}
	if frozen[0].Rate != 0.20 {
		t.Errorf("the second setting froze %v, want the rate as it stood at that write", frozen[0].Rate)
	}
	if set.Parameter != gatepolicy.RiskThreshold {
		t.Errorf("the version that froze the rate names parameter %q", set.Parameter)
	}
}
