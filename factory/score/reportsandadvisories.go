package score

import "context"

// The one outcome that speaks to the exposure factor arrives on the prior
// instead: a later advisory or report on the author's work. The exposure factor
// learns from no outcome of its own — the signals the learning pass reads are
// misbehaviour signals, and a change that posts credentials outward moves
// neither error rate nor latency — so a window closing passed over such a
// release says nothing about it, and what does say something arrives afterwards
// and lands here.

// ReportsAndAdvisories is what reads them. It is an interface for the reason
// [GroupedReports] is: a report is a field of a store of its own that this
// package does not read, and an advisory is a feed no record of the graph
// carries. So the composition hands the score whatever answers it.
type ReportsAndAdvisories interface {
	// OnReleases is every release a later advisory or report arrived on, keyed
	// by release id. It answers the whole set rather than one release at a
	// time because the prior reads every release of every item its author
	// wrote a version of, and a call per release would be one join per release.
	OnReleases(ctx context.Context) (map[string]bool, error)
}

// NoReportsAndAdvisories reads none. It is the value a composition with no such
// reader hands in, and what [New] composes for a nil one. It is not the same as
// a reader that failed: a factory with no report store and no advisory feed has
// nothing to read, and that is an empty answer rather than an unavailable one —
// the distinction [NoMarks], [NoWithdrawals] and [NoGroupedReports] already keep.
type NoReportsAndAdvisories struct{}

// OnReleases is nothing arrived.
func (NoReportsAndAdvisories) OnReleases(context.Context) (map[string]bool, error) {
	return nil, nil
}
