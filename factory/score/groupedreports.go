package score

import "context"

// Whether the intent an item answers carries text the factory did not author
// that no gate has admitted, and whether a report grouped into it says a
// person is being harmed by the software.
//
// An intent's own source answers the first question where it was raised from
// reports; an intent an owner typed answers it too where the grouper has
// since grouped a report into it, because that intent now carries the same
// untrusted text. Both read through this seam, because whether reports were
// grouped in at all is a fact of a report and a report is in a store of its
// own no record of the graph carries.
//
// The mark is read beside the group and takes the source's own treatment: it
// adds no gate a report did not already meet — an intent a group's count
// resolves that row on its own account too, however the mark reads — and
// what it adds is the fact on the vector, so the human deciding sees which
// report is marked.

// ReportGroup is what an intent's grouped reports amount to without their
// words: how many there are, and the ids of the ones marking harm.
type ReportGroup struct {
	Reports int
	Marked  []string
}

// GroupedReports is what reads it. It is an interface because the group is a
// fact of a report, and a report is in a store of its own that this package
// does not read: the score reads records of the factory's graph, and no
// record there carries either question. So the composition hands the score
// whatever answers it.
type GroupedReports interface {
	// Grouped is the group of reports grouped into intentID: how many there
	// are, and the ids of the ones marking harm.
	Grouped(ctx context.Context, intentID string) (ReportGroup, error)
}

// NoGroupedReports reads no group. It is the value a composition with no
// such reader hands in, and what [New] composes for a nil one. It is not the
// same as a reader that failed: that would be an unavailable input and would
// resolve the factors on that account, and this is an empty one — the
// distinction [NoMarks] and [NoWithdrawals] already keep.
type NoGroupedReports struct{}

// Grouped is no reports and no marks.
func (NoGroupedReports) Grouped(context.Context, string) (ReportGroup, error) {
	return ReportGroup{}, nil
}
