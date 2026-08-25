// Package environment owns the environment record: where software runs, as a
// record rather than a name in code.
//
// One record type with a kind fixed at creation. The design has three —
// production, a deploy target a customer defines, and a candidate's own — and
// the CHECK in [DDL] lists the two this milestone writes. A customer's own
// arrives with the screen an owner defines it on. What that costs is a CHECK
// widened once per kind, which is the arrangement package item's stage column
// already has.
//
// # What is on it
//
// The targets are the addresses a deploy is performed against, and they are a
// field rather than records of their own: nothing holds a reference to a target
// that has to survive an address change, a deploy record being keyed by service
// and environment. The credential a deploy is performed with is a
// [secretref.Ref], so the record names it and holds no value. The gate
// thresholds are a table of their own, one row per gate row an owner authored
// one for, because eight rows would otherwise be eight columns and a row a
// milestone has not built would be a column nothing writes.
//
// Three fields are a candidate's alone and empty on a persistent kind. The item
// is what the environment belongs to — the item and not the build, because the
// environment persists across a rebuild. What it was composed from is the
// current release of each of the candidate's dependencies as it was when the
// environment was composed, rewritten at each re-verification. The teardown time
// is written when the item merges, is dropped, or is superseded by a re-decomposition, and
// the row is kept rather than deleted because the deploy records naming it would
// otherwise point at nothing.
//
// # Who may write what
//
// The kind is the seam, and there are two writers on either side of it.
// [Writer] is an owner at Factory: it creates production's record at the
// creation of the project — which an owner does not choose, production existing
// everywhere — and refuses the candidate kind. [SetGateThreshold] is an owner
// authoring a threshold on a persistent record, called by package policy inside
// the transaction that appends the policy version, and it refuses a candidate's
// record: that record is created at the gate that decides its deploy, so it
// cannot hold the threshold that decided it. [Candidates] is the deploy agent,
// the one component that reaches a deploy target at all, and it writes the
// candidate kind and nothing else.
//
// The score writes nothing here: where the threshold is unauthored the value in
// force is what the score supplies, and the supplied value is a field of the
// score's own record.
//
// [CountLiveCandidates] is read against a ceiling that is not gate policy. A
// substrate with no room for another environment holds the candidate deploy gate,
// and the design says of that condition that no parameter of an owner's limits it
// — it is the factory's own infrastructure ceiling. So the number it is compared
// against belongs to whatever composes the deploy agent and not to this package
// and not to package gatepolicy.
//
// What defines it:
// ../../end-goal/how-humans-do-it/05-environments/01-records-and-one-long-lived-branch.md,
// which sets the kind as the seam between writers, the targets as a field, and
// what a persistent kind holds;
// ../../end-goal/how-humans-do-it/05-environments/02-an-environment-per-candidate.md
// for the candidate kind, its composition, and its teardown; and the threshold's
// scope is ../../end-goal/how-humans-do-it/09-gate-policy.md#one-shape-across-all-of-them.
package environment
