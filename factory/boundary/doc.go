// Package boundary is the analysis window's boundary: the arithmetic that turns a
// size and a confidence into a line the health monitor may read as often as it
// likes without the reading losing its meaning.
//
// # Why a boundary rather than a threshold
//
// The comparison is evaluated as traffic arrives, which is a test read
// continuously over a growing sample. A threshold set for one look at a fixed
// sample is not the threshold for a thousand looks at a growing one: read that
// way it crosses eventually whatever the data say, and a factory built on it
// rolls back healthy releases all day. What this package provides instead is a
// pair of one-sided tests whose guarantee holds at every point they are read,
// which is what makes continuous reading legitimate rather than a fault nobody
// traces to its cause.
//
// # The construction
//
// The quantity is a count of units of work and how many of them failed. Two
// rates are named: the baseline rate, estimated from what the release's
// baseline emitted, and the alternative, which is that rate plus the window's
// size — the smallest regression worth catching, as a share of the work.
// [Boundary.Alternative] says why the size is a share of the work rather than of
// the baseline, and what that costs.
//
// One number decides both exits. [Boundary.Evaluate] computes the log of the
// likelihood ratio between the alternative and the baseline over the units
// observed so far. Under the baseline the ratio itself is a non-negative
// martingale with mean one, so by Ville's inequality the chance that it ever
// reaches one over one-minus-confidence — however often it is looked at — is at
// most one-minus-confidence. That crossing is harm. Under the alternative the
// reciprocal is a martingale with mean one by the same argument, so the same
// crossing in the other direction is clean: a regression of the size the window
// was opened to catch has been ruled out at the confidence it was opened with.
// So the two exits are one statistic against a symmetric pair of lines, and the
// confidence is what each of them crosses at rather than a number applied
// afterwards.
//
// [UnitsToClean] and [UnitsToHarm] are the same arithmetic read forwards: how
// many units a release running at a given rate is expected to need before it
// crosses. They exist because the design makes a claim about this — that the
// traffic a comparison needs scales as the inverse square of what it must
// detect — and a claim about the arithmetic should be a property of the
// arithmetic rather than a sentence beside it.
// TestTheUnitsNeededScaleAsTheInverseSquareOfTheSize is that property checked.
//
// # What it costs
//
// The baseline rate is estimated and then treated as known. So the confidence
// this package promises is the boundary's and not the pair's: a baseline read
// from few units carries its own error and nothing here accounts for it. That
// is one half of the confound a started control removes — the other half being
// age, which no arithmetic answers — and on a substrate that starts no control
// both are simply present. The estimate is smoothed by half a failure in one
// unit, because a baseline that failed nothing would otherwise make the ratio
// infinite and fail the first failure a release ever has.
//
// A baseline with no units at all is no baseline, and neither exit is reachable
// from one: [Reading.Unavailable] says so, which is the design's statement that
// a service's first release cannot be passed and can be failed only by a
// threshold an owner stated absolutely. A baseline rate so high that raising it
// by the size passes one is the same answer for the opposite reason — a service
// whose normal behaviour leaves no room above it to detect a regression in.
//
// Who may write what: this package owns no table, reaches no database, and
// imports nothing. It is arithmetic over numbers its caller has already read.
//
// What defines it: the window's boundary, its size and its confidence, and the
// requirement that the boundary be valid at every point it is read are
// ../../end-goal/how-humans-do-it/08-operations.md#the-analysis-window; the four
// exits are the table in that section, of which this package decides two and
// the elapsed cap and a rollback aimed below decide the other two. What the
// comparison is made against is
// ../../end-goal/how-humans-do-it/08-operations.md#the-health-monitor, whose weak
// fallback reads a release against the last known-good release's recent history
// where no control is running.
package boundary
