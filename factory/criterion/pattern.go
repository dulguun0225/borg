package criterion

import "strings"

// Pattern is which of the six EARS sentence forms a criterion's sentence
// fits, or [PatternNoPattern] for a sentence fitting none.
type Pattern string

const (
	// PatternAlwaysTrue is `The system shall <response>`.
	PatternAlwaysTrue Pattern = "always_true"
	// PatternEvent is `When <trigger>, the system shall <response>`.
	PatternEvent Pattern = "event"
	// PatternState is `While <state>, the system shall <response>`.
	PatternState Pattern = "state"
	// PatternUnwantedCondition is `If <condition>, then the system shall
	// <response>`.
	PatternUnwantedCondition Pattern = "unwanted_condition"
	// PatternOptionalFeature is `Where <feature is included>, the system
	// shall <response>`.
	PatternOptionalFeature Pattern = "optional_feature"
	// PatternStateWithAnEventInsideIt is `While <state>, when <trigger>, the
	// system shall <response>`, the sixth pattern.
	PatternStateWithAnEventInsideIt Pattern = "state_with_an_event_inside_it"
	// PatternNoPattern is a sentence fitting no pattern, admitted with a
	// tagged reason and counted — because a form everything can escape is not
	// a form. [Classify] never returns it; [Insert] assigns it.
	PatternNoPattern Pattern = "no_pattern"
)

// Patterns is every pattern a criterion may have. The CHECK constraint in
// [DDL] lists the same seven, and TestDDLListsEveryPattern fails if the two
// lists stop agreeing.
var Patterns = []Pattern{
	PatternAlwaysTrue, PatternEvent, PatternState,
	PatternUnwantedCondition, PatternOptionalFeature, PatternStateWithAnEventInsideIt,
	PatternNoPattern,
}

// Classify is which of the six patterns the sentence fits, or false for a
// sentence fitting none. It is deterministic string matching on the
// keywords, case-insensitive, so what a sentence classifies as is decided by
// its text and by nothing that reads it. A false is not a refusal: the
// caller admits the sentence only with a tagged reason, as [PatternNoPattern].
//
// A sentence beginning `While ` is checked as the state with an event inside
// it before state, because the state form is a prefix of the longer one and
// checking them the other way round would never return it.
func Classify(sentence string) (Pattern, bool) {
	s := strings.ToLower(sentence)
	const shall = ", the system shall "
	switch {
	case strings.HasPrefix(s, "the system shall "):
		return PatternAlwaysTrue, true
	case strings.HasPrefix(s, "when ") && strings.Contains(s, shall):
		return PatternEvent, true
	case strings.HasPrefix(s, "while "):
		when := strings.Index(s, ", when ")
		if when >= 0 && strings.Contains(s[when:], shall) {
			return PatternStateWithAnEventInsideIt, true
		}
		if strings.Contains(s, shall) {
			return PatternState, true
		}
		return "", false
	case strings.HasPrefix(s, "if ") && strings.Contains(s, ", then the system shall "):
		return PatternUnwantedCondition, true
	case strings.HasPrefix(s, "where ") && strings.Contains(s, shall):
		return PatternOptionalFeature, true
	}
	return "", false
}
