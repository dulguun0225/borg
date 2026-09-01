// Package score is the risk score: a vector of named factors, reduced to one
// number by a published formula, computed once per gate firing — and the loop
// that moves what the score supplies as outcomes arrive.
//
// Both halves of the number matter. The number is what a gate compares against
// the threshold in force; the vector is what a human reads when they disagree
// with the number, so every factor carries the quantity it was read from, the
// level that quantity resolved to, the weight the formula gave it, and — where the
// score could not compute it — the reason. A score nobody can argue with is a
// score nobody will trust, and [Rules] is that same rule applied to the seven
// numbers this package supplies.
//
// # What it reads
//
// Every factor but two comes from records this package reads: the releases in an
// item's area, the closed decisions in the log, the artifact the build was made
// from and its author with the outcomes of that author's releases, the releases
// the service already has, and the contracts it publishes with the consumer
// contracts naming them. The two that do not are the size and reach of the change,
// which are read from the build's diff — measured where the repository is, by the
// component that built, and handed here in [Measurement]. It is not stored,
// because the vector computed from it is: a diff re-taken later against a
// repository other items have merged into is not the diff the decision was made
// on, and a vector is written where it was computed and never recomputed.
//
// [decisionlog.ClosedDecisions] is read whole for every assessment, which is what
// a per-author prior over one author's outcomes costs while the log is small. A
// query narrowed by the payload's own fields is what a log that has grown needs,
// and that would put the payload's shape inside the log.
//
// # Empty evidence is a wide value
//
// The per-author prior and the context group's business-area factor start wide
// for an author or an area the factory has not seen, and narrow as outcomes
// arrive. That is not the same as a factor being unavailable: an unavailable
// factor resolves to the top of the scale and the formula gates the change
// outright, and treating an empty history that way would put a human at every
// gate of a new install forever.
//
// What narrows the prior is every outcome on that author's own work — a human's
// verdict on a version it wrote, a analysis window closing over a release of an item
// it wrote, and a human undoing one after it shipped — so a prior keeps moving on
// a factory that has stopped putting humans at gates, which is what it could not
// do until the windows were built and read here.
//
// # The version, and what learning moves
//
// [Version] is a record of the score's own, append-only, naming the published
// formula, the factor set, the published rules, and every value the score
// supplies where an owner authored nothing — six of gate policy's seven rows,
// and none for the list of allowed predicate kinds, which no outcome teaches.
// Every decision names the version in force.
//
// [Writer.Ensure] computes the supplied table from every outcome in the store and
// appends a version where what it computed, or what the source publishes, has
// stopped matching the newest stored version. So a supplied value moving and a
// change to the formula both move the version by one path, and starting the
// factory twice over an unchanged store appends nothing.
//
// A supplied value is per subject and not per parameter: the window limit and the
// analysis window's
// three are supplied per service, the item-size target per area, the attempt limit
// per stage, and the threshold per gate row — the same key the authored value has,
// because what the score supplies is what stands where that field is empty. The
// version stores the starting value of each parameter and a row for every subject
// an outcome has moved it for, so a factory with no outcomes in it has a table of
// seven rows. What it costs is the design's own stated cost arriving in full: a
// long sequence of versions, most of them differing in one number that the
// decisions naming them never read.
//
// Learning is a pass and never a write at a firing. An outcome arrives long after
// the decision it judges, so nothing at a gate could have computed one, and a
// version that moved mid-process would leave two decisions of one run naming
// different numbers. What that costs is that a run acts on what the store said
// when it started.
//
// # Both ends of each parameter are the evidence
//
// Every value moves by harm on one side and by its own stated cost on the other.
// Gate policy's own table says what goes wrong at each end of each of them, and
// both ends are inputs: one end is something going wrong, the other is the
// parameter costing more than it returns. Five of the seven move both ways on that
// reading — the threshold through the sample, the attempt limit on attempts that
// never needed the retry, the cap on how long a window actually takes, the size on
// what the traffic can resolve, and the window limit, which the design already gave both
// directions.
//
// The window's size is where that mattered most. What harm asks for gets finer per
// miss and never coarser; what the traffic reaches is the finest size on that same
// lattice whose units to clean this service's newest closed window's read supplies
// inside the cap. The size in force is the coarser of the two, because a size finer
// than the traffic can resolve rules nothing out: the window ends at the cap every
// time, protects nothing, and holds the next deploy for the whole cap. The two are
// different questions — what is worth catching, and whether anything can be caught
// — and only the first is about harm, which is why reading the second is not the
// analysis window's harm-only restriction being reopened.
//
// Two move one way, and [Rules] states the reason with each: no outcome here
// teaches that a confidence was too high, and nothing measures the other end of an
// item-size target. Neither compounds — the units a window needs grow as the log of
// one over one-minus-confidence, and nothing reads the item-size target yet — which
// is what makes a ratchet on those two tolerable where a ratchet on the size was
// not.
//
// # The held-out sample
//
// [Score.HoldOut] is the sample: one firing in ten that the score would have gated
// is held out instead, and the item auto-passes every gate the score would have
// gated from that firing onward. It exists to break the one self-reinforcement
// the score cannot get out of on its own — a gated change a human approved is not
// evidence the gate was unnecessary, because the human's own scrutiny is part of
// why it turned out well, so the only unbiased evidence for raising a threshold is
// a change the score wanted gated and did not gate.
//
// Two things bound it. It passes nothing a safeguard put a human at, because a
// human a safeguard added at a gate is a human an owner added and nothing in the
// design removes one.
// And it selects an item and not a firing, so the selection is read forward off
// the decisions already opened on that item rather than drawn again at each row.
//
// [NeverDraw] is therefore a factory whose thresholds can only fall. That is a
// legitimate composition — a test asserting a human at a row cannot run against a
// sample that might remove one — and it is not a silent state: the pass says so
// where nothing has been held out.
//
// What this substrate cannot give it is the strategy that keeps a control: every
// deploy here moves a process rather than traffic, so a held-out release is
// watched by the same confounded comparison as every other and the longest watch
// available — the window running to the cap — is the whole of what the sample gets.
// A held-out release is therefore evidence that a comparison was available, and
// not that an unsampled release on the same author would have read the same.
//
// Who may write what: this package owns the score version table and appends to
// it through [Writer]. It writes nothing else and reads every other record
// through the owning package's readers.
//
// What defines it: ../../end-goal/how-the-factory-works/04-risk-score/README.md — the three
// factor groups, the vector recorded where it was computed, what an
// uncomputable factor resolves to, the two halves kept apart until the last
// step, the score version as a record of the score's own, the loop of
// ../../end-goal/how-the-factory-works/04-risk-score/02-how-it-learns.md, and the
// held-out sample with the two fields it is recorded in. The values it supplies
// are the rows of
// ../../end-goal/how-the-factory-works/09-gate-policy/01-what-is-in-it.md; the window
// limit's own
// evidence is ../../end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md
// and the window's size and cap are
// ../../end-goal/how-the-factory-works/08-operations/02-the-analysis-window.md.
package score
