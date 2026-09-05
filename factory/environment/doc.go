// Package environment owns the environment record: where software runs, as a
// record rather than a name in code.
//
// # The files
//
// environment.go is [Environment] with [Environment.Live], [Environment.Addresses]
// and [Environment.EveryTargetServesAShare], [Kind] with [Kinds] and
// [ProductionName], [Target], [Platform] and [Spec], and the reads [Get], [ByName],
// [Production], [ForItem] and [CountLiveCandidates]. writer.go is the persistent
// kinds' writer: [Writer] and [NewWriter] with [Writer.Create], [Writer.Withdraw],
// [Writer.AddTarget] and [Writer.RemoveTarget], the transaction-taking functions
// beside each ([Insert], [Withdraw], [AddTarget], [RemoveTarget]), and
// [SetMaxConcurrentCandidateEnvironments]. candidate.go is the candidate kind's
// writer: [Candidates] and [NewCandidates] with [Candidates.Compose] and
// [Candidates.Recompose], plus [Composition], [Composed] and [NameForItem].
// cycle.go is the compose-and-reclaim cycle: [Reason] with [Reasons] and
// [Reason.ForGood], [Rate], [Cycle] with [Cycle.Open] and [Cycle.Hours],
// [EnvironmentHours], [Candidates.TearDown] and [Candidates.RunCouldStart], and
// the read [Cycles]. targets.go is how the targets and the composition are
// written into and read back out of one field each. threshold.go is
// [SetGateThreshold] and [GateThreshold]. schema.go is [Table], [CycleTable],
// [ThresholdTable], [IDPrefix], [CycleIDPrefix], [ThresholdIDPrefix] and [DDL].
//
// The tests are split by subject: db_test.go holds the fixtures and the
// persistent kinds an owner writes, target_test.go a persistent environment's
// targets and its withdrawal, candidate_test.go the candidate kind's
// composition, cycle_test.go the compose-and-reclaim cycle and what it costs,
// and threshold_test.go the gate threshold — every one of them against the
// database except the cycle's own arithmetic, which needs none.
//
// One record type with a [Kind] fixed at creation. [Kinds] and the CHECK in
// [DDL] list the same three, so a kind is added by widening a CHECK — the
// arrangement package item's stage column already has.
//
// The targets are an ordered list of an address and whether the platform behind
// it serves a share, and they are a field rather than records of their own:
// nothing holds a reference to a target that has to survive an address change,
// a deploy record being keyed by service and environment. [Writer.AddTarget]
// appends and [Writer.RemoveTarget] removes, each refused while a deploy record
// still marks that address complete for a release, and the last target may not
// be removed — an environment with no address is one no deploy can reach, so an
// environment down to one target is withdrawn instead of emptied.
//
// The credential a deploy is performed with is a [secretref.Ref], so the record
// names it and holds no value. The two persistent kinds also declare a
// [Platform]: its name, the credential a candidate environment is composed
// through, and whether it can compose one on demand — [Insert] refuses a
// production environment whose platform cannot, an environment per candidate
// being the shape the design admits and nothing else. A candidate's environment
// declares no platform of its own: it is composed on the platform its item's
// production environment declares. [SetMaxConcurrentCandidateEnvironments]
// authors the ceiling on production's record beside the platform it declares,
// one per platform, and [CountLiveCandidates] is scoped to the production
// environment named — an install whose projects run on two platforms adds
// neither count across them. The project is required of a persistent kind and
// empty on a candidate's, which belongs to the item rather than the project;
// production is one record per project, enforced by a unique index on the
// project where the kind is production.
//
// The gate thresholds are a table of their own, one row per gate row an owner
// authored one for, read by [GateThreshold], rather than eight columns.
//
// Three fields are a candidate's alone and empty on a persistent kind. The item
// is what the environment belongs to — the item and not the build, because the
// environment persists across a rebuild. [Composition] is what the deployer put
// in place beside the candidate: the current release of each dependency as it
// was when the environment was last composed, plus the version of the seed and
// of the non-production value set the store and the configuration were built
// from; [Composition.Equal] compares all three, which is what the merge queue
// does between a run and the run it re-verifies. The externals a candidate
// reaches are not stored here: an external is reached through the
// non-production value set alone, so what this record holds about it is the
// version of that set. TornDownAt is written by [Candidates.TearDown] at one of
// the three teardown-for-good events and keeps the row, because the deploy
// records naming it would otherwise point at nothing; [Environment.Live] is the
// read of it.
//
// A compose-and-reclaim cycle is a row of its own in [CycleTable], one per time
// the environment was composed: when composing began and when the run could
// start, both written by the deployer as it does the work, when it was torn
// down and why, and what that span converted to in environment-hours where the
// service's rate was in force at the write — fixed there and never repriced.
// [Reason.ForGood] tells the three teardown-for-good events apart from a
// reclamation, which the deployer performs on an item running nothing so that
// the platform's room is not consumed by an item waiting on a human; a
// reclamation ends the cycle and leaves the environment row standing, and
// [Candidates.Recompose] opens the next cycle when the item next reaches
// [Deploy to candidate environment]. An environment has at most one open cycle,
// which the partial unique index in [DDL] enforces. [EnvironmentHours] sums
// [Cycle.Hours] across every cycle an item's environment went through.
//
// WithdrawnAt is a persistent kind's own end, an owner's write at Factory
// refused while a deploy record still marks a target of it complete for a
// release; a candidate's is a candidate's of nothing, being torn down instead.
//
// # Who may write what
//
// The kind is the seam, and there are two writers on either side of it.
// [Writer] is an owner at Factory: [Writer.Create] creates a persistent
// environment and refuses the candidate kind, [Writer.Withdraw] ends one, and
// [Writer.AddTarget] and [Writer.RemoveTarget] change its targets.
// [SetGateThreshold] is an owner authoring a threshold on a persistent record,
// called by package policy inside the transaction that appends the policy
// version, and it refuses a candidate's record: that record is created at the
// gate that decides its deploy, so it cannot hold the threshold that decided
// it. [Candidates] is the deployer, the one component that reaches a deploy
// target at all, and it writes the candidate kind and nothing else — composing,
// recomposing, reclaiming and tearing it down. The score writes nothing here:
// where the threshold is unauthored the value in force is what the score
// supplies, and the supplied value is a field of the score's own record.
//
// [CountLiveCandidates] is read against a ceiling that is not gate policy and
// no parameter of an owner's own, so the number it is compared against belongs
// to whatever composes the deployer and not to this package and not to package
// gatepolicy.
//
// What defines it:
// ../../end-goal/how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md,
// which sets the kind as the seam between writers, the targets as a field, and
// what a persistent kind holds;
// ../../end-goal/how-the-factory-works/05-environments/02-an-environment-per-candidate/README.md
// for the candidate kind and its composition;
// ../../end-goal/how-the-factory-works/05-environments/02-an-environment-per-candidate/02-reclaiming-an-environment.md
// for reclamation and the four reasons a cycle ends;
// ../../end-goal/how-the-factory-works/05-environments/02-an-environment-per-candidate/03-room-and-what-an-environment-costs.md
// for the compose timestamps, the platform's room, and environment-hours; and
// the threshold's scope is
// ../../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md.
package environment
