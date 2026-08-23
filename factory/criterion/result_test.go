package criterion

import (
	"strings"
	"testing"
)

// TestDecideIsUndecidedWhereTheRunsDisagree is the third outcome and the reason
// the encodings run twice: an encoding that produced a failure and a pass over the
// same build decided nothing, so the criterion is undecided for that build rather
// than passed.
func TestDecideIsUndecidedWhereTheRunsDisagree(t *testing.T) {
	for _, decided := range []struct {
		first, second bool
		want          Outcome
	}{
		{true, true, OutcomePassed},
		{false, false, OutcomeFailed},
		{true, false, OutcomeUndecided},
		{false, true, OutcomeUndecided},
	} {
		if got := Decide(decided.first, decided.second); got != decided.want {
			t.Errorf("Decide(%v, %v) = %s, want %s", decided.first, decided.second, got, decided.want)
		}
	}
}

// TestUndecidedBlocksLikeAFailure: undecided is read at the Merge to master gate the way a
// failure is, which is the whole reason it is not a kind of pass — a passing
// criterion is all that gate reads about the item's own behaviour.
func TestUndecidedBlocksLikeAFailure(t *testing.T) {
	for outcome, want := range map[Outcome]bool{
		OutcomePassed:    false,
		OutcomeFailed:    true,
		OutcomeUndecided: true,
	} {
		if got := outcome.Blocks(); got != want {
			t.Errorf("%s.Blocks() = %v, want %v", outcome, got, want)
		}
	}
}

// TestDDLListsEveryOutcome keeps the outcome CHECK and [Outcomes] from
// disagreeing: the constraint is SQL text rather than built from the slice, so
// nothing but a test holds the two together.
func TestDDLListsEveryOutcome(t *testing.T) {
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
		if len(listed) != len(Outcomes) {
			t.Fatalf("the constraint lists %d outcomes, Outcomes has %d", len(listed), len(Outcomes))
		}
		for n, o := range Outcomes {
			if got, want := strings.TrimSpace(listed[n]), "'"+string(o)+"'"; got != want {
				t.Errorf("the constraint lists %s where Outcomes has %s", got, want)
			}
		}
	}
	if !found {
		t.Fatalf("no statement carries the %q list", open)
	}
}
