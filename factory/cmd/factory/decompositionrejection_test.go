// Tests of a decomposition rejection: it supersedes the whole set and
// counts a re-decomposition on the intent.
package main

import (
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
)

// TestADecompositionRejectionSupersedesTheSetAndCountsAReDecomposition: the row can stop a bad
// decomposition and cannot repair one — the re-decomposition needs a stage that
// decides the decomposition rather than one told what to produce, and this interface is told.
func TestADecompositionRejectionSupersedesTheSetAndCountsAReDecomposition(t *testing.T) {
	ctx, d, out := newPathOn(t, "reject this should have been three items\n", theService, theSecondService)
	d.model = &contractModel{}

	res, err := run(ctx, d, []asked{across(pairStatement, theService, theSecondService)})
	if err != nil {
		t.Fatalf("the run stopped, and a rejected decomposition is the gate working: %v\noutput:\n%s", err, out)
	}
	if len(res.decompositions) != 1 {
		t.Fatalf("the run decomposed %d sets", len(res.decompositions))
	}
	set := res.decompositions[0]
	if !set.decided || set.approved {
		t.Fatalf("the row decided=%v approved=%v, want a rejection", set.decided, set.approved)
	}
	if set.reDecompositions != 1 {
		t.Fatalf("the intent stands at %d re-decompositions, want the one this rejection counted", set.reDecompositions)
	}

	// Every item of the set is superseded, and none of them reached a stage below
	// decomposition: nothing was authored against a set the gate refused.
	for _, c := range res.candidates {
		if !c.superseded {
			t.Errorf("item %s survived the rejection", c.itemID)
		}
		if c.environmentID != "" || c.merged {
			t.Errorf("item %s reached an environment or a merge after the set was rejected", c.itemID)
		}
		it, err := item.Get(ctx, d.pool, c.itemID)
		if err != nil {
			t.Fatalf("reading item %s: %v", c.itemID, err)
		}
		if it.Stage != item.StageSuperseded {
			t.Errorf("item %s is at %s, want superseded", c.itemID, it.Stage)
		}
		if len(it.SupersededBy) != 0 {
			t.Errorf("item %s points at %v, and no re-decomposition replaced it", c.itemID, it.SupersededBy)
		}
	}
	// The count is on the intent and in a field of its own beside the interview's
	// rounds, because the two are different stretches of work.
	in, err := intent.Get(ctx, d.pool, res.candidates[0].intentID)
	if err != nil {
		t.Fatalf("reading the intent: %v", err)
	}
	if in.ReDecompositions != 1 || in.Rounds != 0 {
		t.Errorf("the intent stands at %d re-decompositions and %d rounds", in.ReDecompositions, in.Rounds)
	}
	if !strings.Contains(out.String(), "the re-decomposition itself is not built") {
		t.Errorf("the run does not say what a rejected decomposition leaves:\n%s", out)
	}
}
