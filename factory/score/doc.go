// Package score is the risk score — a vector of named factors reduced to one
// number by a published formula, computed once per gate firing — and the pass
// that moves what the score supplies as outcomes arrive.
//
// # The code
//
// score.go is [Score], [New] and [Score.Assess], with [Change], [Measurement],
// [ExposureEvidence] and [FleetChange] — what the caller hands in — and
// [OpenEvent] and [CloseEvent], the parts of a decision's payloads this package
// reads back. factor.go is [Factor], [Group] and [Term]; factorsets.go is
// [FactorSet], the three sets, [Weights] and the weights the product ships;
// formula.go is [Formula], [FormulaVersion] and [Assessment]. factorread.go
// reads the change, author and context factors out of the records, exposure.go
// the exposure factor out of the evidence handed in, and prior.go the per-author
// prior with the width and the count of resolved window exits behind it.
// resolution.go is [Resolution] and the [Cause] of each: a resolved factor is
// left out of the weighted means, and a firing that resolved anything is a
// human's whatever the number reads.
//
// windowhistory.go is the [Evidence] methods read off the windows and the
// stages, unexported, that windowlearn.go, rejection.go and learn.go fold: a
// service's window and rollback history in time order, the finest size and the
// timed-out run its traffic reached, how long a window took to resolve, and an
// area's or a stage's stalls and successes.
//
// version.go is [Version] — a row of the decision log, appended through
// [decisionlog.Writer.AppendScoreVersion] and read back by shape — with
// [Writer], [Writer.Ensure], [Writer.Recalibrate], [Writer.EnterShipped], the
// reads [Newest], [Get] and [All], and [InForceAt], which is the version that
// decides a gate an authored threshold binds. supplied.go is [Supplied],
// [SuppliedValues], [Starting], [StartingValues] and [QuantitySubject].
//
// learn.go is the pass — [Learn] over the store, [LearnFrom] over an [Evidence],
// both answering a [Learned]. evidence.go is [ReadEvidence] and the [Outcome] of
// each item; windowlearn.go moves the analysis window's size, power and cap and
// the window limit; rejection.go resolves a human's rejection one of four ways
// and publishes the [FalseAlarm]s; bands.go is the [Band]s of the number;
// drift.go is the two calibration readings and the [Drift] each publishes;
// fit.go is [Fit], the weights a recalibration refits; rules.go is [Rules] and
// [LearningVersion]. Learning is a pass and never a write at a firing, so every
// decision of one run names the version the run started with.
//
// sample.go is the held-out sample: the [Draw] interface with [RandomDraw] and
// [NeverDraw], [Score.HoldOut] and the [Selection] it writes, and the reads
// [HeldOut] and [HeldOutItems]. It selects an item and not a firing, and it
// passes nothing a safeguard put a human at and nothing a resolution did.
//
// marks.go is [Marks], what a named human at Ops marked as not caused by the
// release. The record's writer is Ops and no package owns it yet, so the
// composition hands the score whatever reads it and [NoMarks] is what a factory
// with no such record composes.
//
// Two inputs arrive as parameters because nothing writes them yet:
// [ExposureEvidence], which the component that built the change derives per
// toolchain the way it takes the diff, and [FleetChange], which is the fleet's
// own records. A factory that fires without either resolves the factor rather
// than reading it as nothing.
//
// # The shape
//
// This package owns no table, which is where it departs from the shape a record
// package has: the score version is a row of the decision log, so there is no
// schema.go and no DDL, and package postgres does not name it. What it writes is
// that row and nothing else, and it reads every other record through the owning
// package's readers.
//
// Who may write what: this package appends score versions to the log through
// [decisionlog.Writer]. It writes no record.
//
// What defines it: the four factor groups, the score version, the resolutions
// and the three factor sets are
// ../../end-goal/how-the-factory-works/04-risk-score/01-factors-at-least.md; the
// loop, the held-out sample, the bands and the drift readings are
// ../../end-goal/how-the-factory-works/04-risk-score/02-how-it-learns.md; the
// values it supplies are the rows of
// ../../end-goal/how-the-factory-works/09-gate-policy/01-what-is-in-it.md and
// their scopes are
// ../../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md;
// the window limit and the mark are
// ../../end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md;
// the window's size, power and cap are
// ../../end-goal/how-the-factory-works/08-operations/02-the-analysis-window.md;
// the hazard severity the context group reads is
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/03-hazard-severity.md;
// and the score version as a row of the chained log is seam 2 of
// ../../end-goal/deferred.md.
package score
