package intent

import "strings"

// Pattern is which of the six EARS sentence forms a requirement's statement
// fits. A statement fitting none is stored with an empty pattern and a tagged
// escape reason, and [Escaped] is what counts those.
//
// The six are the same six a criterion takes, EARS being a form for
// requirements before it is one for criteria, and the names and the spellings
// here are the ones package criterion uses: a defect found in one is found in
// the other by one search. The two lists are copies rather than one import,
// which is the arrangement deps.txt sets for this package.
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
)

// Patterns is the six a requirement's statement may fit. The CHECK constraint
// in [DDL] lists the same six and the empty pattern beside them, and
// TestDDLListsEveryKindAndPattern fails if the two stop agreeing.
var Patterns = []Pattern{
	PatternAlwaysTrue, PatternEvent, PatternState,
	PatternUnwantedCondition, PatternOptionalFeature, PatternStateWithAnEventInsideIt,
}

// Classify is which of the six patterns the statement fits, or false for one
// fitting none. It is deterministic string matching on the keywords,
// case-insensitive, so what a statement classifies as is decided by its text
// and by nothing that reads it. A false is not a refusal: the writer admits
// the statement with a tagged escape reason instead.
//
// A statement beginning `While ` is checked as the state with an event inside
// it before state, because the state form is a prefix of the longer one and
// checking them the other way round would never return it.
func Classify(statement string) (Pattern, bool) {
	s := strings.ToLower(statement)
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
