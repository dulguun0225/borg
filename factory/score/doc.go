// Package score is the risk score: a vector of named factors, reduced to one
// number by a published formula, computed once per gate firing.
//
// Both halves matter. The number is what a gate compares against the threshold
// in force; the vector is what a human reads when they disagree with the
// number, so every factor carries the quantity it was read from, the level that
// quantity resolved to, the weight the formula gave it, and — where the score
// could not compute it — the reason. A score nobody can argue with is a score
// nobody will trust.
//
// # What it reads
//
// Every factor but two comes from records this package reads: the releases in
// an item's area, the closed decisions in the log, the artifact the build was
// made from and its author, the releases the service already has, and the
// contracts it publishes with the declarations naming them. The two
// that do not are the size and reach of the change, which are read from the
// build's diff — measured where the repository is, by the component that built,
// and handed here in [Measurement]. It is not stored, because the vector
// computed from it is: a diff re-taken later against a repository other items
// have merged into is not the diff the decision was made on, and a vector is
// written where it was computed and never recomputed.
//
// [ClosedDecisions] is read whole for every assessment, which is what an
// authorship prior over one author's outcomes costs while the log is small. A
// query narrowed by the payload's own fields is what a log that has grown
// needs, and that would put the payload's shape inside the log.
//
// # Empty evidence is a wide value
//
// The authorship prior and the context group's business-area factor start wide
// for an author or an area the factory has not seen, and narrow as outcomes
// arrive. That is not the same as a factor being unavailable: an unavailable
// factor resolves to the top of the scale and the formula gates the change
// outright, and treating an empty history that way would put a human at every
// gate of a new install forever.
//
// What narrows them is a human's verdict and nothing else. A watch window
// closing without harm is not built yet, and an auto-passed decision is the
// factory agreeing with itself rather than evidence about the author. The cost
// is the sharpest thing in this package: a prior narrowed by human verdicts
// stops moving once the factory stops putting humans at gates, which is the
// self-reinforcement the design holds out a random sample to break — and there
// is no sample here, so nothing records a held-out selection either.
//
// # The version
//
// [Version] is a record of the score's own, append-only, naming the published
// formula, the factor set, and every value the score supplies where an owner
// authored nothing — six of gate policy's seven rows, and none for the predicate
// catalog, which no outcome teaches. Every decision names the version in force.
// [Writer.Ensure] appends one where what the source publishes has stopped
// matching the newest stored version, which is how a change to the formula moves
// the version; the formula here is authored rather than learned, so that is one
// row until learning moves it.
//
// Who may write what: this package owns the score version table and appends to
// it through [Writer]. It writes nothing else and reads every other record
// through the owning package's readers.
//
// What defines it: ../../end-goal/how-humans-do-it/04-risk-score.md — the three
// factor groups, the vector recorded where it was computed, what an
// uncomputable factor resolves to, the two halves kept apart until the last
// step, and the score version as a record of the score's own. The values it
// supplies are the rows of
// ../../end-goal/how-humans-do-it/09-gate-policy.md#what-is-in-it.
package score
