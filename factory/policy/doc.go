// Package policy is Factory's writer and the value in force.
//
// # One writer for everything an owner authors
//
// Every authored value in the factory is a field of the record its scope names,
// and every one of those records names Factory as the writer of that field: the
// gate thresholds on an environment, the window limit and the analysis window's
// parameters on a service, the item-size target on an area, the attempt limit
// and the list of allowed predicate kinds on the factory-wide settings record,
// and every safeguard. [Factory] is that writer. The record packages each
// expose a call taking a transaction, and this package is the one caller of all
// of them, so one component holds the whole of duty 8 and the records keep one
// writer each.
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
// decision was decided under is the effective values its own open event
// carries. What that costs is that reading a version back says which write it
// was and not what the whole policy said at that moment, which is answerable
// only by replaying the writes before it.
//
// # The value in force is a read of three things
//
// An owner authors on the record the scope names, the score supplies where the
// field is empty, and a safeguard clamps the result. That order is the whole
// rule: a safeguard is a bound and not a precedence, so a ceiling caps the value
// and a floor raises it and neither replaces a value already narrower than
// itself. The one parameter whose safeguard is not arithmetic is the risk
// threshold — a safeguard on it adds a human at the gate rather than moving the
// number — which is why [Applied] carries both a threshold and whether a
// safeguard adds a human.
//
// What the score supplies is read out of the score version this reader was
// composed with, and per subject: the score supplies the window limit for one
// service and the
// item-size target for one area, which is the same key the authored field has. A
// [Reader] holds that version rather than reading the newest at each answer,
// because a supplied value moves as outcomes arrive — a reader that re-read it
// could hand one gate firing a threshold from a version its own decision row does
// not name, and a decision has to be readable against the policy it was decided
// under. The zero version is the starting values, which is what a factory that has
// appended none supplies.
//
// The direction of that edge is the other half of the rule. This package reads the
// score and the score reads no authored value: what an owner wrote is an override
// of what the score supplies, so a score that read the authored value would be
// supplying a number against what an owner had already decided.
//
// One parameter has a fourth read under the other three, and it is the list of
// allowed predicate kinds: the kinds the factory itself can decide. Gate policy
// has an owner extend the list and a safeguard only add to it, which
// presupposes something to extend, and the score supplies none — no outcome
// teaches a kind of assertion. So the value in force is the factory's own
// kinds, extended by what an owner authored and again by each safeguard, and
// [FromFactory] is the source a printer reports where an owner authored
// nothing. It is the one parameter with that source.
//
// [Reader.SafeguardPredicatesOn] is beside those reads and is not one of them. A
// safeguard's predicate binds no parameter's value: it adds an assertion on one
// element of one contract, so what it resolves to is a list of assertions rather
// than a number. It is here because package safeguard has one reader and this is
// it.
//
// Which subjects a safeguard is read on is the mechanism's question and not the
// parameter's: a gate firing reads the gate row, the item's service, and every
// area in the item's chain, because a safeguard on any of them reaches the
// firing. A safeguard drawn on a subject no mechanism queries for that
// parameter applies to nothing, which is the dangling safeguard the design
// already accounts for.
//
// # What has no reader yet
//
// One of the eight resolves here and is read by nothing: the item-size target,
// which waits for a decomposition that sizes anything. [Effective] says so per
// parameter, so an owner who authors it is told that it changes nothing yet
// rather than finding out by its having no effect. The list of allowed
// predicate kinds was the last of the others to get a reader, which is the
// derivation of a consumer contract.
//
// Who may write what: this package owns the policy version table and appends to
// it. Every other write it makes is a call into the package that owns the
// record, inside its own transaction.
//
// What defines it: ../../end-goal/how-the-factory-works/09-gate-policy/README.md — the
// seven rows, the scope of each, the score supplying what an owner does not, a
// safeguard being a bound, and Factory as the writer. The policy version on
// every decision is ../../end-goal/what-the-factory-does/02-traceability.md.
package policy
