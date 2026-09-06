// TestAdmissionIsOrderedByTierThenPriority is the order dispatch admits items
// in where more is ready than the infrastructure allows. It shares db_test.go's
// fixtures and its package.
package dispatch_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
)

// TestAdmissionIsOrderedByTierThenPriority: dispatch orders admission by the
// tier of the intent each item was decomposed from, greater first, and an
// item's own priority breaks a tie within one tier.
func TestAdmissionIsOrderedByTierThenPriority(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{Text: aSpec}}, nil, 3)

	low := c.tiered(t, 1, 0, "sv_low")
	highLowPriority := c.tiered(t, 3, 1, "sv_high_a")
	highHighPriority := c.tiered(t, 3, 9, "sv_high_b")

	ordered, err := c.dispatch.Admit(c.ctx, []item.Item{low, highLowPriority, highHighPriority})
	if err != nil {
		t.Fatalf("Admit: %v", err)
	}
	want := []string{highHighPriority.ID, highLowPriority.ID, low.ID}
	for n, it := range ordered {
		if it.ID != want[n] {
			t.Fatalf("the order is %s at %d, want %s — tier first, priority within one",
				it.ID, n, want[n])
		}
	}

	// A tier orders admission and nothing else: the list it is given is the
	// list it answers with, one item at a time and none dropped.
	if len(ordered) != 3 {
		t.Errorf("Admit answered %d items, want the three it was given", len(ordered))
	}
}

// tiered is one item of an intent at the tier given, with the priority given on
// the item. The tier is written at the arrival, which is where a detector's
// intent carries one; the priority is dispatch's own write.
func (c composed) tiered(t *testing.T, tier, priority int, evidence string) item.Item {
	t.Helper()
	in, err := c.intake.TakeIn(c.ctx, record.Actor{Kind: record.KindComponent, Key: "health_monitor"},
		intent.Arrival{
			Source: intent.SourceDetector, Statement: "a defect is live", ProjectID: oneProject,
			Evidence: intent.Evidence{ServiceID: evidence},
			Tier:     intent.Tier{Value: tier, PolicyVersion: "dl_00000000000000000000000000000000"},
		})
	if err != nil {
		t.Fatalf("TakeIn: %v", err)
	}
	it, err := c.decomposition.Create(c.ctx, decompositionActor, item.New{
		IntentID: in.ID, ServiceID: oneService, AreaID: oneArea, Branch: "item/" + evidence,
	}, oneProject, oneProject, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if priority == 0 {
		return it
	}
	it, err = c.items.SetPriority(c.ctx, record.Actor{Kind: record.KindHuman, Key: "person:owner", Basis: record.BasisClaimed},
		it.ID, priority)
	if err != nil {
		t.Fatalf("SetPriority: %v", err)
	}
	return it
}
