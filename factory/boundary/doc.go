// Package boundary is the analysis window's boundary: the arithmetic that turns
// a size and a confidence into a pair of one-sided tests the health monitor may
// read as often as it likes without the reading losing its meaning.
//
// boundary.go is all of it. [Boundary] is the size and the confidence, with
// [Boundary.Validate], [Boundary.Alternative] — the baseline rate plus the size,
// the smallest regression worth catching as a share of the work — and
// [Boundary.Crossing], the line one over one-minus-confidence puts both exits
// at. [Observed] is what was read: the units and the failures of the release and
// of its baseline. [Boundary.Evaluate] computes the log of the likelihood ratio
// between the alternative and the baseline over the units observed so far and
// returns a [Reading] saying harm, clean, or neither, so the two exits are one
// statistic against a symmetric pair of lines. [Formula] names that construction
// and is the string a window record copies.
//
// [Reading.Unavailable] is neither exit being reachable, for one of two reasons:
// [NoBaseline], a release with nothing below it to be compared against, and
// [NoHeadroom], a baseline failing so often that the size raises it past every
// unit failing. The baseline rate is estimated and then treated as known, and it
// is smoothed by half a failure in one unit, because a baseline that failed
// nothing would otherwise make the ratio infinite.
//
// [Boundary.UnitsToClean] and [Boundary.UnitsToHarm] are the same arithmetic
// read forwards — how many units a release running at a given rate is expected
// to need before it crosses — so that the design's claim about how traffic
// scales is a property of the arithmetic rather than a sentence beside it;
// TestTheUnitsNeededScaleAsTheInverseSquareOfTheSize is that property checked.
// [ErrNoCrossing] is a rate that drifts toward the other exit instead.
//
// Who may write what: this package owns no table, reaches no database, and
// imports nothing. It is arithmetic over numbers its caller has already read.
//
// What defines it: the window's boundary, its size and its confidence, the
// requirement that the boundary be valid at every point it is read, and the four
// exits of which this package decides two are
// ../../end-goal/how-the-factory-works/08-operations/02-the-analysis-window.md. What the
// comparison is made against is
// ../../end-goal/how-the-factory-works/08-operations/01-the-health-monitor.md, whose weak
// fallback reads a release against the last known-good release's recent history
// where no control is running.
package boundary
