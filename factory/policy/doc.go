// Package policy is Factory's writer and the value in force.
//
// # One writer for everything an owner authors
//
// Every authored value in the factory is a field of the record its scope names,
// and every one of those records names Factory as the writer of that field: the
// gate thresholds on an environment, K and the watch window's parameters on a
// service, the item-size target on an area, the attempt bound and the predicate
// catalog on the factory policy record, and every pin. [Factory] is that
// writer. The record packages each expose a call taking a transaction, and this
// package is the one caller of all of them, so one component holds the whole of
// duty 8 and the records keep one writer each.
//
// # The policy version
//
// Every authoring write appends a [Version], in the same transaction as the
// write, and the version in force is the newest row. A decision names it, for
// the reason a decision names an actor: an owner re-authors gate policy, and a
// decision read back against today's values is not the decision that was made.
//
// A version names the write and not the value. The value is the field on the
// record the subject names, one fact in two places could disagree, and what a
// decision was decided under is the effective values its own opening row
// carries. What that costs is that reading a version back says which write it
// was and not what the whole policy said at that moment, which is answerable
// only by replaying the writes before it.
//
// # The value in force is a read of three things
//
// An owner authors on the record the scope names, the score supplies where the
// field is empty, and a pin clamps the result. That order is the whole rule: a
// pin is a bound and not a precedence, so a ceiling caps the value and a floor
// raises it and neither replaces a value already narrower than itself. The one
// parameter whose pin is not arithmetic is the risk threshold — a pin on it adds
// a human at the gate rather than moving the number — which is why [Applied]
// carries both a threshold and whether a human is pinned.
//
// Which subjects a pin is read on is the mechanism's question and not the
// parameter's: a gate firing reads the gate row, the item's service, and every
// area in the item's chain, because a pin on any of them reaches the firing. A
// pin drawn on a subject no mechanism queries for that parameter applies to
// nothing, which is the dangling pin the design already accounts for.
//
// # What has no reader yet
//
// Four of the eight parameters resolve here and are read by nothing: the
// item-size target waits for a cut that sizes anything, the predicate catalog
// for contracts, and K and the window's parameters for the watch window.
// [Effective] says so per parameter, so an owner who authors one is told that it
// changes nothing yet rather than finding out by its having no effect.
//
// Who may write what: this package owns the policy version table and appends to
// it. Every other write it makes is a call into the package that owns the
// record, inside its own transaction.
//
// What defines it:
// ../../end-goal/how-humans-do-it/09-gate-policy.md — the seven rows, the scope
// of each, the score supplying what an owner does not, a pin being a bound, and
// Factory as the writer. The policy version on every decision is
// ../../end-goal/what-the-factory-does.md#traceability.
package policy
