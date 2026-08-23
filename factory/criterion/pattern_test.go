package criterion_test

import (
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/criterion"
)

// TestClassifyTheSixPatterns is one sentence in each form, classified as
// that form.
func TestClassifyTheSixPatterns(t *testing.T) {
	sentences := map[string]criterion.Pattern{
		"The system shall respond within one second.":                                        criterion.PatternAlwaysTrue,
		"When a report arrives, the system shall open an intent.":                            criterion.PatternEvent,
		"While a deploy is running, the system shall refuse a second one.":                   criterion.PatternState,
		"If the credential is unreachable, then the system shall append a wait.":             criterion.PatternUnwantedCondition,
		"Where the service has a user interface, the system shall record its state machine.": criterion.PatternOptionalFeature,
		"While the queue is full, when an item arrives, the system shall hold it outside.":   criterion.PatternStateWithEvent,
	}
	for sentence, want := range sentences {
		got, matched := criterion.Classify(sentence)
		if !matched {
			t.Errorf("Classify(%q) matched nothing, want %s", sentence, want)
			continue
		}
		if got != want {
			t.Errorf("Classify(%q) = %s, want %s", sentence, got, want)
		}
	}
}

// TestClassifyIsCaseInsensitiveOnTheKeywords is the same forms with the
// keywords in other cases.
func TestClassifyIsCaseInsensitiveOnTheKeywords(t *testing.T) {
	got, matched := criterion.Classify("WHEN a report arrives, THE SYSTEM SHALL open an intent.")
	if !matched || got != criterion.PatternEvent {
		t.Errorf("Classify on upper-cased keywords = %s, %v; want %s, true", got, matched, criterion.PatternEvent)
	}
}

// TestClassifyRefusesASentenceFittingNoPattern is the false a caller admits
// only with a tagged reason.
func TestClassifyRefusesASentenceFittingNoPattern(t *testing.T) {
	for _, sentence := range []string{
		"The checkout page loads fast.",
		"While a deploy is running, nothing else happens.",
		"",
	} {
		if got, matched := criterion.Classify(sentence); matched {
			t.Errorf("Classify(%q) = %s, want no match", sentence, got)
		}
	}
}

// TestStateWithEventIsNotMisreadAsState is the ordering pattern.go states:
// the state form is a prefix of the longer one, so the longer one is checked
// first.
func TestStateWithEventIsNotMisreadAsState(t *testing.T) {
	sentence := "While the window is open, when a page fires, the system shall escalate it."
	got, matched := criterion.Classify(sentence)
	if !matched {
		t.Fatalf("Classify(%q) matched nothing", sentence)
	}
	if got != criterion.PatternStateWithEvent {
		t.Errorf("Classify(%q) = %s, want %s", sentence, got, criterion.PatternStateWithEvent)
	}
}

// TestDDLListsEveryPattern fails if [criterion.Patterns] and the CHECK
// constraint in the DDL stop agreeing.
func TestDDLListsEveryPattern(t *testing.T) {
	for _, pattern := range criterion.Patterns {
		if !strings.Contains(criterion.DDL[0], "'"+string(pattern)+"'") {
			t.Errorf("the DDL's pattern CHECK does not list %q", pattern)
		}
	}
}
