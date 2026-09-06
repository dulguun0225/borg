// Package boundary is the analysis window's boundary: the arithmetic that turns
// a size, a confidence, a power and a count of comparisons into a pair of lines
// the health monitor may read as often as it likes without the reading losing
// its meaning.
//
// boundary.go is the vocabulary. [Version] is the boundary version a window
// copies at the open. [Worse] with [WorseHigher] and [WorseLower] is which
// direction of the difference between the arms is a regression. [Boundary] is
// the size, the confidence, the count of [Boundary.Comparisons] the confidence
// is held over, and that direction, with [Boundary.Validate]. [Counts] is one
// interval's counts for both arms — the units and the count of the quantity on
// each — with [Counts.Add], [Counts.Rate] and [Counts.BaselineRate], and
// [Observed] is the intervals, with [Observed.Totals], which is what a window
// stores its exit against. [Reading] is what one read says.
//
// evaluate.go is [Boundary.Crossing] and [Boundary.Evaluate]. The interval is
// the unit the variance is estimated over and the request is not: each
// interval's counts per arm are one observation, and the spread between
// intervals is the scale the statistic is read at. Nothing here knows which
// quantity it is reading; what the count in an interval is depends on the
// quantity and is the caller's.
//
// [AtRunLength] is the same boundary for a reading that never closes — the
// reading against a service's own recent history, and an explicit threshold —
// where an average run length stands in for the confidence, and
// [Boundary.RunLength] reads it back.
//
// reach.go is the same arithmetic read forwards, which is how the design's claim
// about traffic is a property of the arithmetic rather than a sentence beside
// it. [Boundary.IntervalsToPassed] and [Boundary.IntervalsForPassed] are the
// intervals a window needs to close early, the second at a stated power;
// [Boundary.IntervalsToFailed] is the other exit, and [Boundary.FinestSize] is
// the finest size the intervals actually read could rule out, which is what a
// window records at its close; [Boundary.AtPower] reads a recorded one at
// another power, which is how a caller holding what the traffic reached asks
// whether it supports the power in force. [Boundary.UnitsToPassed] and
// [Boundary.UnitsToFailed] are the second bound, in units of work inside an
// interval, and the stricter of the two governs. [Coarsest] is the size in force
// out of the size asked for and the floors under it. [ErrNoCrossing] is a
// reading that drifts toward the other exit and [ErrPowerUnreachable] is a power
// no count of intervals reaches, which is the window with no passed exit
// available to it.
//
// [Reading.Unavailable] is neither exit being reachable, for one of four
// reasons: [NoVolume], no interval read on both arms — except on a
// [WorseLower] boundary, where an interval the other arm served and the release
// arm counted no units in is read as the release arm having received nothing,
// which is what fails a silent release beside a serving control; [NoBaseline], a release
// with no arm to be compared against; [NoSpread], fewer intervals read than a
// spread between them can be read from — the interval bound stated the other
// way, so a burst of traffic inside one interval buys nothing the next
// interval does not; and [NoHeadroom], a baseline so near the end of the scale
// that the size takes the alternative past it. The rates are estimated and
// then read as known, and each is smoothed by half an occurrence in one unit,
// because an arm that counted nothing would otherwise have no sampling
// variance at all.
//
// The tests are boundary_test.go, the reading itself, and reach_test.go, the
// same arithmetic read forwards; both are arithmetic alone, this package
// reaching no database.
//
// Who may write what: this package owns no table, reaches no database, and
// imports nothing but the standard library. It is arithmetic over numbers its
// caller has already read.
//
// What defines it: the window's boundary, the interval as the unit its variance
// is estimated over, the confidence held over the set of comparisons, the power,
// the size per quantity, and the requirement that the boundary be valid at every
// point it is read are
// ../../end-goal/how-the-factory-works/08-operations/02-the-analysis-window.md.
// The quantities, the arm reading that fails a silent release beside a serving
// control, and the histogram the latency quantile is read from are
// ../../end-goal/how-the-factory-works/08-operations/01-the-health-monitor.md.
// What the comparison is made against without a control is the release below the
// one under watch on that target, which
// ../../end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md
// computes and package healthmonitor reads; it is never the last known-good
// release, which can be a release above the one being read.
package boundary
