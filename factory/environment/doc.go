// Package environment owns the environment record: where software runs, as a
// record rather than a name in code.
//
// # The files
//
// writer.go is [Environment] and [Environment.Live], [Kind] with [Kinds] and
// [ProductionName], [Writer] and [NewWriter] with [Writer.Create],
// [SetGateThreshold] and [GateThreshold], and the reads [Get], [ByName],
// [ForItem] and [CountLiveCandidates]. candidate.go is the other writer:
// [Candidates] and [NewCandidates] with [Candidates.Compose],
// [Candidates.Recompose] and [Candidates.TearDown], plus [Composed] and
// [NameForItem]. targets.go is how the targets and the composition are written
// into and read back out of one field each. schema.go is [Table],
// [ThresholdTable], [IDPrefix], [ThresholdIDPrefix] and [DDL].
//
// The tests are db_test.go, every one of them against the database.
//
// One record type with a [Kind] fixed at creation. [Kinds] and the CHECK in
// [DDL] list the same ones, so a kind is added by widening a CHECK — the
// arrangement package item's stage column already has.
//
// The targets are the addresses a deploy is performed against, and they are a
// field rather than records of their own: nothing holds a reference to a target
// that has to survive an address change, a deploy record being keyed by service
// and environment. The credential a deploy is performed with is a
// [secretref.Ref], so the record names it and holds no value. The gate
// thresholds are a table of their own, one row per gate row an owner authored
// one for, read by [GateThreshold], rather than eight columns.
//
// Three fields are a candidate's alone and empty on a persistent kind. The item
// is what the environment belongs to — the item and not the build, because the
// environment persists across a rebuild. What it was composed from is the
// current release of each of the candidate's dependencies as it was when the
// environment was composed, rewritten at each re-verification. The teardown time
// is written by [Candidates.TearDown], which keeps the row rather than deleting
// it because the deploy records naming it would otherwise point at nothing;
// [Environment.Live] is the read of that field.
//
// # Who may write what
//
// The kind is the seam, and there are two writers on either side of it.
// [Writer.Create] is an owner at Factory: it creates production's record and
// refuses the candidate kind. [SetGateThreshold] is an owner authoring a
// threshold on a persistent record, called by package policy inside the
// transaction that appends the policy version, and it refuses a candidate's
// record: that record is created at the gate that decides its deploy, so it
// cannot hold the threshold that decided it. [Candidates] is the deploy agent,
// the one component that reaches a deploy target at all, and it writes the
// candidate kind and nothing else. The score writes nothing here: where the
// threshold is unauthored the value in force is what the score supplies, and
// the supplied value is a field of the score's own record.
//
// [CountLiveCandidates] is read against a ceiling that is not gate policy and
// no parameter of an owner's, so the number it is compared against belongs to
// whatever composes the deploy agent and not to this package and not to package
// gatepolicy.
//
// What defines it:
// ../../end-goal/how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md,
// which sets the kind as the seam between writers, the targets as a field, and
// what a persistent kind holds;
// ../../end-goal/how-the-factory-works/05-environments/02-an-environment-per-candidate/README.md
// for the candidate kind, its composition, and its teardown; and the threshold's
// scope is ../../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md.
package environment
