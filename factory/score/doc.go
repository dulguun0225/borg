// Package score is the risk score — a vector of named factors reduced to one
// number by a published formula, computed once per gate firing — and the pass
// that moves what the score supplies as outcomes arrive.
//
// score.go is the assessment: [Score] holds the version and the draw, [New]
// composes one, and [Score.Assess] computes an [Assessment] from a [Change] and
// the [Measurement] of the build's diff, which is handed in rather than stored
// because the vector computed from it is stored instead. [OpenEvent] and
// [CloseEvent] are the payloads a gate writes into the log. factor.go is
// [Factor], [Group] and [Half]: each factor carries the quantity it was read
// from, the level that quantity resolved to, the weight the formula gave it,
// and, where the score could not compute it, the reason. An unavailable factor
// resolves to the top of the scale; empty evidence does not — a per-author prior
// or an area the factory has not seen starts wide and narrows as outcomes
// arrive. factorread.go reads each factor out of the records, and formula.go is
// [Formula], [FormulaVersion] and [Assessment.UnavailableFactors].
//
// version.go is [Version] — append-only, naming the published formula, the
// factor set, the published rules, and every value the score supplies — with
// [Writer], [Writer.Ensure], which appends only where what it computed has
// stopped matching the newest stored version, and the reads [Newest], [Get] and
// [All]. supplied.go is [Supplied], [SuppliedValues], [Starting] and
// [StartingValues]: a supplied value is per subject and not per parameter, the
// same key the authored field has. schema.go is [Table], [IDPrefix], [DDL] and
// [AdvisoryLockKey], which serialises the read of the newest version and the
// append that supersedes it.
//
// learn.go is the pass — [Learn] over the store, [LearnFrom] over an [Evidence]
// — evidence.go is [ReadEvidence] and the [Outcome] of each item, windowlearn.go
// moves the analysis window's three per service, and rules.go is [Rules] and
// [LearningVersion], the published statement of how each supplied value moves.
// Learning is a pass and never a write at a firing, so every decision of one run
// names the version the run started with.
//
// sample.go is the held-out sample: [SampleRate], the [Draw] interface with
// [RandomDraw] and [NeverDraw], [Score.HoldOut] and the [Selection] it writes,
// and the reads [HeldOut] and [HeldOutItems]. It selects an item and not a
// firing, so the selection is read forward off the decisions already opened on
// that item, and it passes nothing a safeguard put a human at.
//
// Who may write what: this package owns the score version table and appends to
// it through [Writer]. It writes nothing else, and reads every other record
// through the owning package's readers.
//
// What defines it: the factor groups, the score version and the held-out sample
// are ../../end-goal/how-the-factory-works/04-risk-score/README.md; the loop is
// ../../end-goal/how-the-factory-works/04-risk-score/02-how-it-learns.md; the values it
// supplies are the rows of
// ../../end-goal/how-the-factory-works/09-gate-policy/01-what-is-in-it.md; the window
// limit is ../../end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md;
// and the window's size and cap are
// ../../end-goal/how-the-factory-works/08-operations/02-the-analysis-window.md.
package score
