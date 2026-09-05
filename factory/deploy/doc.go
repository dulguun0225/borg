// Package deploy owns the deploy record and the rollout without a control: one
// record per rollout, written when the deploy starts, reaching the target through
// [targetseam.Target].
//
// # The files
//
// deploy.go is the record and its vocabulary: [Deploy], [Strategy] with
// [Strategies], [Status] with [Statuses], [Undoing] and [Undoing.Any],
// [SourceHealthMonitorAtFailed] and [SourceOfHuman], and [What] with its two
// constructors [OfRelease] and [OfBuild]. writer.go is [Writer] and
// [NewWriter] — [Writer.Start], [Writer.StartUndoing], [Writer.Complete] and
// [Writer.Undo] — and the reads [Get], [Current], [ByRelease], [Rollbacks] and
// [NewestRollback]. withoutcontrol.go is [WithoutControl], the rollout, and
// restore.go is [Restore], the rollback. schema.go is [Table], [IDPrefix] and
// [DDL].
//
// db_test.go is the tests against the database, and schema_test.go is the one
// subject that needs none: the CHECK constraints listing every strategy and
// every status.
//
// A deploy's [Status] moves from started to complete, and from either to rolled
// back where a rollback undid the release. The record is an ordinary record and
// not a row of the log, and nothing chains it, so a status is an update where a
// gate's verdict is a second row.
//
// [Strategy] has two values and only one is ever written.
// [StrategyWithControl] is in the CHECK because the record's definition names
// two strategies, and nothing writes it: a target that runs a release as a local
// process moves a process rather than a share of arriving traffic, so every
// deploy here goes without a control and is started from cold.
//
// # A rollback is this record and not another
//
// [Restore] is the rollback: a deploy of the release being returned to, naming
// on the same record the release it failed, the releases it swept, the source
// that called for it, and the intent it raised. A record of its own was refused
// because a second writer on the fact of what is running is the fact the drift
// detector exists to check. [Undoing.Any] tells a rollback's record from an
// ordinary deploy's, and [SourceOfHuman] records a named human at Ops as the
// source rather than as the actor, the actor being the deploy agent that
// performed the rollback. The failed release stays a field apart from the swept
// ones: one failed release is one revert item, and the swept ones were never
// failed.
//
// # What a record names, and what is current
//
// [What] is the pair: the build the deploy put on the target, on every record,
// and the release the deploy is of, on every record but a candidate's —
// [OfRelease] and [OfBuild] being the two constructors. The build is on every
// record because a target reports the build it runs, which is what makes what
// runs comparable to what the record says.
//
// The record is keyed by service and environment and not by target, so a
// service's current release is single-valued per environment: the one [Current]
// names, the most recently completed deploy — what is running, not what is
// newest.
//
// # What a failed rollout leaves
//
// [WithoutControl] writes the record started, calls the target, and advances the
// record complete. On a target error the record stays started: the call may have
// failed before, during, or after the target acted, so writing either outcome
// would be a guess, and a record saying started about a target that may or may
// not run the release is the disagreement the drift detector reads targets to
// raise. [Restore] leaves the same thing on the same terms, and leaves nothing
// marked undone with it: a store saying a release was rolled back with nothing
// put back in its place would describe a service running nothing.
//
// Who may write what: [Writer] inserts a deploy and advances its status;
// nothing updates any other field and nothing deletes. service_id,
// environment_id, release_id, build_id, and a rollback's four are id fields and
// not foreign keys — a cross-package link is a field the link walk reads, and the
// store checks an id for being present and not for pointing at anything.
//
// What defines it: the deploy record in
// ../../end-goal/how-the-factory-works/06-releases/05-the-deploy-record/README.md — written
// by the agent performing the deploy through seam 4, advancing to complete or
// rolled back, keyed by service and environment; the strategy table in
// ../../end-goal/how-the-factory-works/03-gates/02-the-rollout-strategy.md; what a
// rollback is and what its record names, in
// ../../end-goal/how-the-factory-works/06-releases/06-rollback.md; and which release
// it returns to, in
// ../../end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md,
// computed for one rollback by the health monitor, which is what calls [Restore].
package deploy
