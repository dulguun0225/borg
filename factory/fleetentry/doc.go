// Package fleetentry owns the fleet entry record: a model at an effort in a
// role with a scope, the credential it runs on, the processing location that
// credential resolves to, the classes of material it may be handed, how much
// the model reads at once, and how many dispatches pass between
// evaluation-set runs. The nine fields are the design's whole list; the
// operations an agent may perform stay the role's and never this record's.
//
// schema.go is [Table], [IDPrefix], [FormatVersion] and [DDL]. writer.go holds
// [Entry] and [New] as the writer's input, [Scope] as the two halves of a
// scope, the seven [ClassIntentStatement]-through-[ClassFailureRecords]
// constants and [MaterialClasses], the list they compose; [Writer] and
// [NewWriter], wrapping [Writer.Write] and [Writer.Withdraw] in their own
// transactions; and the readers [Get], [InForce], [InForceForRole] and
// [CoveredRoles].
//
// Who may write what: [Writer] is an owner at Factory, and no component in
// the pipeline writes back to it — the factory reads this table and never
// writes it. A write by any actor but a human is refused with [ErrNotAnOwner].
// [Writer.Withdraw] keeps the row and sets withdrawn_at rather than deleting
// it or flipping a flag; withdrawing an entry already withdrawn is refused
// with [ErrAlreadyWithdrawn].
//
// The role a fleet entry names is checked here only for being present. The
// role vocabulary — the factory's own stages, the two put on an intent, the
// grouper, and the role that argues a fleet proposal — is package dispatch's,
// which this package cannot import without a cycle, so a role dispatch does
// not recognise is a hold dispatch writes and not a refusal this package
// makes. [CoveredRoles] answers which roles at least one entry in force
// names; the comparison against dispatch's full role list is composed
// elsewhere, in dispatch itself.
//
// What is not built: nothing reads dispatches_between_evaluation_runs yet —
// the evaluation set is content the product does not ship at this milestone,
// so the field is stored and read by nothing. The readiness reading per role
// and the spend ceiling's comparison against what an agent run spent are
// composed elsewhere, over this table and others, and are not this package's.
//
// What defines it: the fleet entry itself is
// ../../end-goal/how-the-factory-works/10-fleet/01-what-an-agent-runs-on.md.
// Factory writing it is
// ../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md.
// The role and the scope a fleet entry narrows are
// ../../end-goal/how-the-factory-works/01-one-pipeline.md.
package fleetentry
