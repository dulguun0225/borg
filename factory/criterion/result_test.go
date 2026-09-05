package criterion

import (
	"strings"
	"testing"
)

// TestUndecidedBlocksLikeAFailure: undecided is read at the Merge to master
// gate the way a failure is, which is the whole reason it is not a kind of
// pass — a passing criterion is all that gate reads about the item's own
// behaviour.
func TestUndecidedBlocksLikeAFailure(t *testing.T) {
	for outcome, want := range map[Outcome]bool{
		OutcomePassed:    false,
		OutcomeFailed:    true,
		OutcomeUndecided: true,
	} {
		if got := outcome.Blocks(false); got != want {
			t.Errorf("%s.Blocks(false) = %v, want %v", outcome, got, want)
		}
	}
}

// TestAnUnreliableCriterionBlocksNothing: while a criterion is unreliable its
// failure rejects nothing, counts no attempt, and moves no prior, and Merge to
// master reads it as absent. Its result is still recorded, which is why the
// exception is here and not in the writer.
func TestAnUnreliableCriterionBlocksNothing(t *testing.T) {
	for _, outcome := range Outcomes {
		if outcome.Blocks(true) {
			t.Errorf("%s.Blocks(true) = true, want false: an unreliable criterion is read as absent", outcome)
		}
	}
}

// TestDDLListsEveryObservedOutcome keeps the outcome CHECK and [Observed] from
// disagreeing: the constraint is SQL text rather than built from the slice, so
// nothing but a test holds the two together. Undecided is in [Outcomes] and not
// in [Observed], because no run observes one — it is derived by [Undecided] at
// the read, and the store refuses it.
func TestDDLListsEveryObservedOutcome(t *testing.T) {
	const open = "outcome in ("
	found := false
	for _, statement := range DDL {
		i := strings.Index(statement, open)
		if i < 0 {
			continue
		}
		found = true
		rest := statement[i+len(open):]
		j := strings.Index(rest, ")")
		if j < 0 {
			t.Fatalf("the %q list is not closed", open)
		}
		listed := strings.Split(rest[:j], ",")
		if len(listed) != len(Observed) {
			t.Fatalf("the constraint lists %d outcomes, Observed has %d", len(listed), len(Observed))
		}
		for n, o := range Observed {
			if got, want := strings.TrimSpace(listed[n]), "'"+string(o)+"'"; got != want {
				t.Errorf("the constraint lists %s where Observed has %s", got, want)
			}
		}
	}
	if !found {
		t.Fatalf("no statement carries the %q list", open)
	}
}

// TestDDLListsEveryPlace keeps the place CHECK and [Places] from disagreeing,
// the same way.
func TestDDLListsEveryPlace(t *testing.T) {
	for _, place := range Places {
		listed := false
		for _, statement := range DDL {
			if strings.Contains(statement, "'"+string(place)+"'") {
				listed = true
			}
		}
		if !listed {
			t.Errorf("the DDL's place CHECK does not list %q", place)
		}
	}
}
