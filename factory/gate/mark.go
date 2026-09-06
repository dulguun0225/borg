package gate

// What put a human at a row is a mark on the row and never the absence of one,
// which is what makes it readable without the number. Where a row waiting on a
// human carries none of the five, what sent them is the number against the risk
// threshold, and [MarkTheNumber] is that residual written out.

// Mark is what put a human at a row. A row may carry more than one — a resolved
// factor and a safeguard both, where an owner withdrawing the safeguard would
// not remove the human.
type Mark string

const (
	// MarkResolvedFactor is a vector naming a factor resolved rather than
	// valued, which says the number decided nothing. No threshold an owner
	// authors and no formula the score publishes auto-passes such a firing.
	MarkResolvedFactor Mark = "the vector names a factor resolved rather than valued"
	// MarkSafeguard is a safeguard among the values actually applied, which says
	// an owner put the human there whatever the number was.
	MarkSafeguard Mark = "a safeguard adds a human at this row"
	// MarkEditInPlace is a row an Edit in place fired. It waits on another
	// holder of the row's duty, or on the editor where none exists, however the
	// recomputed number moves.
	MarkEditInPlace Mark = "an edit in place fired this row, so it waits on a holder other than the editor"
	// MarkReviewSample is the review sample having selected the row: the score
	// would have auto-passed it and an authored rate sent it to a human anyway.
	MarkReviewSample Mark = "the review sample selected this row"
	// MarkWithdrawalRow is a row that reads no threshold at all — a safeguard's
	// withdrawal, a halt's withdrawal, a legal hold's withdrawal, and the
	// shortening of decision-log retention. A human is at each of them always.
	MarkWithdrawalRow Mark = "this row reads no threshold: a human decides it always"
	// MarkTheNumber is the residual the design states: a row waiting on a human
	// that carries none of the five above is one the number sent them to. It is
	// written out rather than left as an absence, so that a row with no mark at
	// all is a row with no human at it, and it is written beside another mark
	// where both hold — an owner withdrawing a safeguard would not remove a
	// human the number put there, and a row carrying one mark cannot say so.
	MarkTheNumber Mark = "the number is at or above the threshold in force"
)

// Marks is every mark, in the order the design names them.
var Marks = []Mark{
	MarkResolvedFactor, MarkSafeguard, MarkEditInPlace, MarkReviewSample,
	MarkWithdrawalRow, MarkTheNumber,
}

// marksOn is what put a human at this row, in the order [Marks] lists them, and
// empty where no human is at it.
func marksOn(overThreshold, resolved, bySafeguard, editInPlace, reviewSampled, readsNoThreshold bool) []Mark {
	var marks []Mark
	if resolved {
		marks = append(marks, MarkResolvedFactor)
	}
	if bySafeguard {
		marks = append(marks, MarkSafeguard)
	}
	if editInPlace {
		marks = append(marks, MarkEditInPlace)
	}
	if reviewSampled {
		marks = append(marks, MarkReviewSample)
	}
	if readsNoThreshold {
		marks = append(marks, MarkWithdrawalRow)
	}
	if overThreshold {
		marks = append(marks, MarkTheNumber)
	}
	return marks
}
