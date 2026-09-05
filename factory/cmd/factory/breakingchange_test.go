// Tests of a breaking change rejected at the merge row naming the
// consumer it would break.
package main

import (
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/service"
)

// TestABreakingChangeIsRejectedAtTheMergeRowNamingTheConsumer is the milestone's own
// episode: a candidate that passes every criterion in force and removes an element a
// consumer declares, rejected before a verdict is asked for, with the consumer on the
// row.
func TestABreakingChangeIsRejectedAtTheMergeRowNamingTheConsumer(t *testing.T) {
	ctx, d, out := newContractPath(t)
	pair(t, ctx, d, out)

	res := runOne(t, ctx, d, out, breakStatement, theService)
	c := only(t, res)
	if c.merged {
		t.Fatalf("a candidate that removes an element the consumer declares merged:\n%s", out)
	}
	if !c.autoRejected || c.autoRejectedBy != gate.AutoRejectedByContractDiff {
		t.Fatalf("the candidate was rejected by %q (auto %v), want the producer's own contract diff",
			c.autoRejectedBy, c.autoRejected)
	}
	// Every criterion in force passed: the break is in no criterion's path, which
	// is the shape of defect a criterion cannot see.
	for _, result := range c.criteria {
		if result.Outcome.Blocks() {
			t.Fatalf("criterion %s is %s, and this episode is about a change the criteria cannot see",
				result.CriterionID, result.Outcome)
		}
	}
	if c.checked == nil || c.checked.Passed() {
		t.Fatal("enforcement reports the candidate as passing")
	}
	if !strings.Contains(c.checked.Why(), "Detail") {
		t.Errorf("the rejection does not name the element: %s", c.checked.Why())
	}
	consumer, found, err := service.ByName(ctx, d.pool, theSecondService)
	if err != nil || !found {
		t.Fatalf("reading the consumer: found %v, %v", found, err)
	}
	if !strings.Contains(c.checked.Why(), consumer.ID) {
		t.Errorf("the rejection does not name the consumer it would break: %s", c.checked.Why())
	}

	// The item went back to Implementation with an attempt counted there, which is
	// what a reject at this row does.
	it, err := item.Get(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}
	if it.Stage != item.StageImplementation {
		t.Errorf("the rejected item is at %s, want implementation", it.Stage)
	}
	if !strings.Contains(out.String(), "before a verdict was asked for") {
		t.Errorf("the run does not say the check rejected before a verdict:\n%s", out)
	}
}
