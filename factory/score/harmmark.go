package score

import "context"

// Whether a report grouped into the item's intent says a person is being harmed
// by the software, which resolves at the Spec row beside the source that intent
// came from.
//
// The mark is the reporter's own field and nothing infers it. It adds no gate a
// report did not already meet — an intent grouped from reports resolves the
// source at that same row, so a human confirms the criteria whatever the rest of
// the vector says either way — and what it adds is a fact on the vector, so the
// human deciding sees which one is marked.

// HarmMarks is what reads it. It is an interface because the mark is a field of
// a report, and a report is in a store of its own that this package does not
// read: the score reads records of the factory's graph, and no record there
// carries the mark. So the composition hands the score whatever answers it.
type HarmMarks interface {
	// Marked reports whether any report grouped into the intent carries the
	// mark, and false for an intent no report raised.
	Marked(ctx context.Context, intentID string) (bool, error)
}

// NoHarmMarks reads no mark. It is the value a composition with no such reader
// hands in, and what [New] composes for a nil one. It is not the same as a
// reader that failed: that would be an unavailable input and would resolve the
// factor on that account, and this is an empty one — the distinction [NoMarks]
// and [NoWithdrawals] already keep.
type NoHarmMarks struct{}

// Marked is nothing marked.
func (NoHarmMarks) Marked(context.Context, string) (bool, error) { return false, nil }
