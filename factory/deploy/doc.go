// Package deploy owns the deploy record, its completion per target, and the
// mitigation, and performs the deploy that writes them: one record per rollout,
// written when the deploy starts, reaching the target through
// [targetseam.Target].
//
// # The files
//
// deploy.go is the record and its vocabulary: [Deploy], [Strategy] with
// [Strategies], [Status] with [Statuses] and the Step* constants named beside
// [StatusFailed], [Completion] with [Completions], [Target], [Priced],
// [Snapshot], [Undoing] and [Undoing.Any] with [SourceHealthMonitorAtFailed],
// [SourceSearch] and [SourceOfHuman], [What] with its constructors [OfRelease],
// [OfBuild] and [OfRemoval] and [What.Removal], and [AdvisoryLockKey].
// schema.go is [Table], [TargetTable], [MitigationTable], [IDPrefix],
// [MitigationIDPrefix], [FormatVersion], [FormatVersionMitigation] and [DDL].
// writer.go is [Writer] and [NewWriter] with [Writer.Pool], [Reaching] and
// [Beginning], [Writer.Start] and [Writer.StartUndoing], [Writer.Complete],
// [Writer.MarkFailed] and [Writer.PerformedWithoutControl], and every error
// this writer returns. writetarget.go is the writes per target row:
// [Writer.ReachTarget], [Writer.CompleteTarget], [Writer.TearDownKept],
// [Writer.Undo] and [Writer.UndoTarget]. snapshot.go is the schema change the
// build carries and the copy taken before it:
// [Writer.MarkSchemaChangeComplete], [Writer.NameSnapshot] and
// [Writer.DeleteSnapshot].
//
// read.go is every read that takes the pool and not the writer: [Get],
// [Targets], [CompleteOnEvery], [Current], [ByRelease], [Unfinished],
// [Rollbacks] and [NewestRollback]. rollout.go is the ordinary rollout:
// [Reach], [Bake] and [Notifier] as interfaces the caller implements,
// [Performance] and [Perform], [DigestConfiguration], and the errors
// [ErrSnapshotRefused], [ErrSchemaChangeRefused] and [ErrTargetRefused].
// restore.go is the slow rollback: [Artifacts], [ErrDigestDiffers],
// [Restoration] and [Restore]. resume.go is the restart: [Resume] and
// [Partial]. mitigation.go is what Ops asks for outside a rollout:
// [Operation] with [Operations], [Mitigation] and [Mitigating], [Mitigate],
// [Writer.BeginMitigation] and [Writer.EndMitigation], [Mitigations] and
// [StandingMitigations], and the errors [ErrOperationUnknown],
// [ErrMitigationIncomplete], [ErrMitigationNotFound] and [ErrNotAHuman].
// adoption.go is the two records the deployer writes on another package's
// table: [Adopt] and [Found] on the service record, and [RecordTargetCheck]
// and [RecordPlatformCheck] on the last-check record, plus [Writer.Token] for
// the writes made through another package's transaction-taking write.
//
// db_test.go is the tests against the database, in an external package for the
// reason its own comment states, and holds the fixtures the other test files
// of that package use; read_test.go is what a reader reads as running;
// schema_test.go is the one subject that needs no database: the CHECK
// constraints listing every value; rollout_test.go is the ordered walk, the
// rollback's verification, the restart, and the mitigation, against
// [targetseam.Fake].
//
// # The record
//
// It is keyed by service and environment and not by target: one record names
// one release for the whole environment, and completion is a field per target
// on [TargetTable]. The identity is the pair plus [Deploy.Number], a sequence
// [Writer.Start] assigns per pair under an advisory lock, so a rollout, a
// rollback, a revert's deploy, and a deploy the search calls for each write
// one record for the same pair and none collide. service_id, environment_id,
// release_id, build_id and the id lists are id fields and not foreign keys:
// record's doc.go states that rule and its cost once.
//
// A deploy names what it put on the targets as [What]: the build always,
// where one was put, and the release except on the three [OfBuild] and
// [OfRemoval] admit — a candidate's own deploy, a search's, and a removal,
// which clears the current release once complete everywhere. [Status] moves
// from started to complete, or to failed at one of the named steps; a record
// with some targets complete and some not stays started as a recorded partial
// deploy, which [Resume] and the drift detector both read rather than treating
// as a mismatch.
//
// [Strategy] attaches to a production deploy and to no other:
// strategy_picked is what the score chose and strategy_performed what the
// deployer performed, which differ where a target declared as serving a share
// refuses the shift — [Writer.PerformedWithoutControl] is that write.
// control_target names the target the control ran on under a strategy with
// one.
//
// # A rollback is this record and not another
//
// [Restore] is the slow rollback: a deploy of the release being returned to,
// naming on the same record the release it failed, the releases it skipped,
// and the source that called for it — [Undoing]. A record of its own was
// refused because a second writer on the fact of what is running is the fact
// the drift detector exists to check. Where a control kept the earlier release
// running, the fast rollback is a traffic shift and not this path at all;
// [Restore] is what a rollout without a control, or a control's own target,
// leaves. It verifies the build's artifact digest against what the target
// reports before deploying anything, so a redeploy by name never restores
// bytes other than the ones that were verified.
//
// [Resume] is the restart: it completes a record every target of which
// finished, marks a record no target reached failed at [StepStopped], and
// returns every record in between for the caller to carry on reaching the rest
// of. It reads only the started records, a failed one being left alone.
//
// [Mitigation] is a record and composes the columns every record table does.
// It is not a deploy: it is what Ops asks the deployer to perform on a target
// outside a rollout, on a human's instruction, and the drift detector reads a
// standing one as intended state that differs from the deploy record on
// purpose. [Writer] writes all three tables and nothing else writes any of
// them.
//
// # What the deployer writes elsewhere
//
// [Adopt] writes the service record's four reachability fields at adoption
// and at every first release, through that package's own writer inside this
// package's transaction — the service record has three writers and the field
// is the seam between them. [RecordTargetCheck] and [RecordPlatformCheck]
// write the deployer's last check per persistent target and per platform. The
// caller assembles [Found] from what the deploy just did; the one input this
// package cannot see is the emission the health monitor reads, which is behind
// an interface of its own and doc.go says which caller supplies it.
//
// # What is not built yet
//
// [Performance.Bake] takes an interface nothing implements: the health monitor
// is what could answer it, and a rollout given none holds nowhere between
// targets. [Performance.BakeVolume] is a field on the service record in the
// design that record does not carry yet, so the caller supplies it here.
// way_in_token_digest is written at every deploy and read by nothing: the way
// in that would send the token to the deployed service, and the report store
// that would digest it, are not built. [Mitigating.Principal] is the deployer's
// own principal, supplied by the caller — the command-line interface, until
// Ops is a screen.
//
// Who may write what: [Writer] inserts a deploy, its targets, and a
// mitigation, and advances each in place; nothing updates any other field and
// nothing deletes.
//
// What defines it: the deploy record in
// ../../end-goal/how-the-factory-works/06-releases/05-the-deploy-record/README.md
// — written by the deployer through seam 4, advancing to complete or failed,
// keyed by service and environment; the strategy table in
// ../../end-goal/how-the-factory-works/03-gates/02-the-rollout-strategy.md;
// what a rollback is and what its record names, in
// ../../end-goal/how-the-factory-works/06-releases/06-rollback.md, and which
// release the slow one returns to, in
// ../../end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md,
// computed by the health monitor, which is what calls [Restore]; the restart
// and the deployer's write order in ../../end-goal/one-process.md; and the
// mitigation in
// ../../end-goal/how-the-factory-works/08-operations/09-the-deployer.md.
package deploy
