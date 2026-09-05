package gate

// What put a human at a row: the score's number, a safeguard, and a drift
// mismatch, and the one word or combination of words an open event names it by.

// The two reasons a human decides, in the words the open event stores.
const (
	// WhyOverThreshold is the number being at or above the threshold in force.
	WhyOverThreshold = "the number is at or above the threshold in force"
	// WhySafeguard is a safeguard adding a human, which a safeguard may do
	// whatever the number reads.
	WhySafeguard = "a safeguard adds a human at this row"
	// WhyBoth is both at once, which is worth telling apart from either: an
	// owner withdrawing the safeguard would not remove the human.
	WhyBoth = "the number is at or above the threshold in force, and a safeguard adds a human"
	// WhyMismatch is a record the drift detector found disagreeing with what runs. It
	// is the one reason a human decides that is neither the score's nor an owner's,
	// and it is appended to whichever of the three above also holds — an owner
	// clearing the mismatch would not remove a human the number put there.
	WhyMismatch = HoldDriftMismatch
)

// why is what put a human at the row. The score's number and a safeguard are the
// two the design gives every row, and their four combinations are the three
// constants above; a mismatch is appended rather than replacing either, because
// clearing it would not remove a human the number put there.
func why(overThreshold, bySafeguard, mismatch bool) string {
	reason := ""
	switch {
	case overThreshold && bySafeguard:
		reason = WhyBoth
	case overThreshold:
		reason = WhyOverThreshold
	case bySafeguard:
		reason = WhySafeguard
	}
	switch {
	case !mismatch:
		return reason
	case reason == "":
		return WhyMismatch
	default:
		return reason + ", and " + WhyMismatch
	}
}
