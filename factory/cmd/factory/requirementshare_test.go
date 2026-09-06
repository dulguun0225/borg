// What each item of a decomposition answers: a requirement one item answers
// alone is assigned to it whole, and one the split spreads over several is
// assigned to none of them and derived into a share per item.
package main

import (
	"context"
	"testing"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
)

// TestOneItemAnswersTheIntentsRequirementsWhole: a decomposition yielding one
// item fires no Decomposition row and every requirement of that intent is that
// item's by construction — so it is assigned whole, no share is derived, and
// the criteria the spec introduces name what the item answers.
func TestOneItemAnswersTheIntentsRequirementsWhole(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)

	res, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	c := only(t, res)

	it, err := item.Get(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}
	reading, err := intent.Requirements(ctx, d.pool, c.intentID)
	if err != nil {
		t.Fatalf("reading the requirements: %v", err)
	}
	if len(reading) != 1 || reading[0].Kind != intent.KindConfirmed {
		t.Fatalf("the reading is %+v, want the one statement the requester confirmed", reading)
	}
	if len(it.RequirementsAnswered) != 1 || it.RequirementsAnswered[0] != reading[0].ID {
		t.Errorf("the item answers %v, want the intent's requirement whole", it.RequirementsAnswered)
	}
	shares, err := intent.ForItem(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the shares: %v", err)
	}
	if len(shares) != 0 {
		t.Errorf("%d share(s) were derived, and a set of one item answers the whole", len(shares))
	}
	assertCriteriaNameWhatTheItemAnswers(t, ctx, d, c.svc.ID, c.itemID, it.RequirementsAnswered)
}

// TestASplitAcrossTwoItemsDerivesAShareEach: a requirement the split spreads
// over several items is assigned to none of them, decomposition derives one
// requirement per item pointing at the one it came from, and each item's
// criteria name its own share — which is what the Spec row rejects in both
// directions over.
func TestASplitAcrossTwoItemsDerivesAShareEach(t *testing.T) {
	ctx, d, out := newContractPath(t)
	res := pair(t, ctx, d, out)

	if len(res.candidates) != 2 {
		t.Fatalf("the run has %d candidates, want the two items of the pair", len(res.candidates))
	}
	whole, err := intent.Requirements(ctx, d.pool, res.candidates[0].intentID)
	if err != nil {
		t.Fatalf("reading the requirements: %v", err)
	}
	var confirmed []intent.Requirement
	for _, r := range whole {
		if r.Kind == intent.KindConfirmed {
			confirmed = append(confirmed, r)
		}
	}
	if len(confirmed) == 0 {
		t.Fatal("the reading holds no confirmed requirement to spread over the set")
	}

	for _, c := range res.candidates {
		it, err := item.Get(ctx, d.pool, c.itemID)
		if err != nil {
			t.Fatalf("reading item %s: %v", c.itemID, err)
		}
		if len(it.RequirementsAnswered) != 0 {
			t.Errorf("item %s answers %v whole, and a requirement the split spreads is assigned to none of them",
				c.itemID, it.RequirementsAnswered)
		}
		shares, err := intent.ForItem(ctx, d.pool, c.itemID)
		if err != nil {
			t.Fatalf("reading the shares of %s: %v", c.itemID, err)
		}
		if len(shares) != len(confirmed) {
			t.Fatalf("item %s carries %d share(s), want one per requirement the split spreads", c.itemID, len(shares))
		}
		ids := make([]string, 0, len(shares))
		for _, share := range shares {
			if share.Kind != intent.KindDerived || share.DerivedFrom == "" || share.ItemID != c.itemID {
				t.Errorf("the share is %+v, want a derived requirement of this item pointing at what it came from", share)
			}
			ids = append(ids, share.ID)
		}
		assertCriteriaNameWhatTheItemAnswers(t, ctx, d, c.svc.ID, c.itemID, ids)
	}
}

// assertCriteriaNameWhatTheItemAnswers is the Spec row's own reading over what
// the run wrote: every requirement assigned to the item is named by a criterion
// in force for it, and no criterion in force for it names a requirement
// assigned elsewhere.
func assertCriteriaNameWhatTheItemAnswers(t *testing.T, ctx context.Context, d deps, serviceID, itemID string, assigned []string) {
	t.Helper()
	inForce, err := criterion.InForce(ctx, d.pool, serviceID, []string{itemID})
	if err != nil {
		t.Fatalf("reading the criteria in force for %s: %v", itemID, err)
	}
	named := make([]string, 0, len(inForce))
	for _, one := range inForce {
		named = append(named, one.RequirementID)
	}
	if check, found, rejects := gate.SpecRejection(assigned, named); rejects {
		t.Errorf("the Spec row would reject item %s by %q: %s", itemID, check, found)
	}
}
