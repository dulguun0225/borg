package intent_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/intent"
)

// TestThePatternsAreSpeltAsPackageCriterionSpellsThem is what the duplication
// pays for: the six names and the six stored spellings are copies of package
// criterion's, so a defect found in one is found in the other by one search.
// The literals here are the copy; a rename in either package that is not made
// in both fails this test.
func TestThePatternsAreSpeltAsPackageCriterionSpellsThem(t *testing.T) {
	want := []intent.Pattern{
		"always_true",
		"event",
		"state",
		"unwanted_condition",
		"optional_feature",
		"state_with_an_event_inside_it",
	}
	if len(intent.Patterns) != len(want) {
		t.Fatalf("there are %d patterns, want %d", len(intent.Patterns), len(want))
	}
	for n, pattern := range intent.Patterns {
		if pattern != want[n] {
			t.Errorf("pattern %d is %q, want %q", n, pattern, want[n])
		}
	}
}

// TestClassifyTheSixPatterns is one statement in each form, classified as that
// form, the sixth included: the state form is a prefix of the longer one, so
// the longer one is checked first.
func TestClassifyTheSixPatterns(t *testing.T) {
	statements := map[string]intent.Pattern{
		"The system shall retry a failed charge.":                                        intent.PatternAlwaysTrue,
		"When a charge fails, the system shall retry it once.":                           intent.PatternEvent,
		"While a retry is running, the system shall refuse a second one.":                intent.PatternState,
		"If the card is declined, then the system shall show the reason.":                intent.PatternUnwantedCondition,
		"Where the shop has a basket, the system shall keep it for a day.":               intent.PatternOptionalFeature,
		"While the basket is open, when a charge fails, the system shall retry it once.": intent.PatternStateWithAnEventInsideIt,
	}
	for statement, want := range statements {
		got, matched := intent.Classify(statement)
		if !matched {
			t.Errorf("Classify(%q) matched nothing, want %s", statement, want)
			continue
		}
		if got != want {
			t.Errorf("Classify(%q) = %s, want %s", statement, got, want)
		}
	}
}
