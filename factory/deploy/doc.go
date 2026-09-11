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
// [Snapshot], [Undoing] and [Undoing.Any] with [SourceHealthMonitorAtFailed]
// and [SourceOfHuman], [What] with its constructors [OfRelease],
// [OfBuild] and [OfRemoval] and [What.Removal], and [AdvisoryLockKey].
// schema.go is [Table], [TargetTable], [MitigationTable], [IDPrefix],
// [MitigationIDPrefix], [FormatVersion], [FormatVersionMitigation] and [DDL].
// writer.go is [Writer] and [NewWriter] with [Writer.Pool], [Writer.Complete],
// [Writer.MarkBackfillCopied], [Writer.MarkFailed],
// [Writer.PerformedWithControl] and [Writer.PerformedWithoutControl], and every
// error this writer returns.
// start.go is what begins a deploy: [Reaching], [Beginning], [Writer.Start] and
// [Writer.StartUndoing]. writetarget.go is the writes per target row:
// [Writer.ReachTarget], [Writer.CompleteTarget], [Control] with
// [Writer.ControlStarted], and [Writer.UndoTarget].
// instancehours.go is the three fleets' spans: [Writer.TearDownRelease],
// [Writer.TearDownControl], [Writer.TearDownKept] and [Hours]. snapshot.go is
// the schema changes the build carries, the copy taken before them and the
// deletion of that copy: [Writer.MarkSchemaChangesComplete],
// [Writer.NameSnapshot], [Writer.MarkSnapshotDeleted], [Deleting] with
// [DeleteSnapshot], and [Pass] with [DeleteExpiredSnapshots] and
// [ExpiredSnapshots].
//
// read.go is every read that takes the pool and not the writer: [Get],
// [Targets], [CompleteOnEvery], [Current], [CurrentOnTarget],
// [PreviousOnTarget], [BackfillComplete], [ByRelease], [Unfinished],
// [Rollbacks] and [NewestRollback]. bake.go is the hold between
// one target and the next: [Bake] as an interface the caller implements,
// [DefaultBakePoll], and the hold itself. configuration.go is what the deployer
// hands the service and what the record says about it: [DigestConfiguration]
// and [WayInTokenName]. rollout.go is the ordinary rollout: [Reach] and
// [Notifier], [Performance] and [Perform], and the errors
// [ErrSnapshotRefused], [ErrTargetRefused] and
// [ErrTargetNotOfTheEnvironment]. schemastep.go is the step before
// traffic — the store brought to what the build declares, the snapshot before a
// destructive change, and the adoption's changes written as found applied — with
// [ErrSchemaChangeRefused]. restore.go is the two rollbacks: the fast one,
// [Returning] and [ShiftBack] with [ErrNothingKeptToReturnTo], and the slow
// one, [Artifacts], [ErrDigestDiffers], [ErrSchemaChangeAtARollback],
// [Restoration] and [Restore]. resume.go is the restart:
// [Reading] and [Rebuilding] as interfaces the caller implements, [Rebuilt],
// [Resume] and [Partial]. mitigation.go is what Ops asks for outside a rollout:
// [Operation] with [Operations], [Mitigation] and [Mitigating], [Mitigate],
// [Writer.BeginMitigation] and [Writer.EndMitigation], [Mitigations] and
// [StandingMitigations], and the errors [ErrOperationUnknown],
// [ErrMitigationIncomplete], [ErrMitigationNotFound] and [ErrNotAHuman].
// retirement.go is what an owner's write of retired calls the deployer for:
// [Removal], [Environment], [Remove] and [ErrRemovalIncomplete].
// adoption.go is the two records the deployer writes on another package's
// table: [Adopt] and [Found] on the service record, and [RecordTargetCheck]
// on the last-check record — its platform record is
// lastcheck.Writer.RecordPlatformPass and not here — plus [Writer.Token] for
// the writes made through another package's transaction-taking write.
//
// db_test.go is the tests against the database, in an external package for the
// reason its own comment states, and holds the fixtures the other test files
// of that package use; read_test.go is what a reader reads as running;
// schema_test.go is the one subject that needs no database: the CHECK
// constraints listing every value. Against [targetseam.Fake]: rollout_test.go
// is the ordered walk and the strategy performed, and holds the fakes the
// files beside it share; recordfields_test.go is what the record carries
// beside a target's completion — the configuration digest, the way-in token
// digest, the delivered releases, and the copy named on a change that
// destroys; schemastep_test.go is the store step; restore_test.go is the slow
// rollback's verification and the fast one's shift onto the kept instances;
// resume_test.go is what each advances target by target and the restart;
// mitigation_test.go is the mitigation and retirement_test.go the removal.
// instancehours_test.go is the three fleets' spans and the backfill mark.
//
// # The record
//
// It is keyed by service and environment and not by target: one record names
// one release for the whole environment, and beside each of the environment's
// targets it says whether that target has it yet, which is a row of
// [TargetTable]. The rollout is the service's own set of those targets, so a row
// for a target the service does not run on carries [Target.NotRunHere] and stays
// not reached, and what completes the record is every other row. The identity is the pair plus [Deploy.Number], a sequence
// [Writer.Start] assigns per pair under an advisory lock, so a rollout, a
// rollback, a revert's deploy, and a deploy the search calls for each write
// one record for the same pair and none collide. service_id, environment_id,
// release_id, build_id and the id lists are id fields and not foreign keys:
// record's doc.go states that rule and its cost once.
//
// The record's actor is the deployer that performed it and nothing else:
// deploying is not a stage an agent is dispatched to, and a human who called for
// one is the record's source, which is [Undoing.Source]. [Writer.Start] refuses
// any other actor with [ErrNotTheDeployer].
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
// The control is no part of what a deploy names at the start: there is one per
// production target the release has reached, started on that target when the
// rollout reaches it, so [Writer.ControlStarted] writes the release it runs, the
// build that release is, and the instances running it on that target and then.
// A target the rollout never reached names none, and a deploy that ran no
// comparison there is on the record as one.
//
// [Strategy] attaches to a production deploy and to no other:
// strategy_picked is what the score chose and strategy_performed what the
// deployer performed. Three things make them differ, and none of the three
// refuses the deploy: a service's first release, which has no control whatever
// the score prefers; a target whose platform serves no share, where the row is
// unavailable permanently rather than once; and a target declared as serving a
// share that refuses the shift. [Writer.PerformedWithoutControl] is that write,
// and [Writer.PerformedWithControl] is the shift returning. Neither is written at
// the start: a deployer that stopped before it performed anything would
// otherwise leave a record naming a control that never ran. The control itself
// is a target row's own field and not the whole deploy's:
// [TargetTable].control_release_id names the release the control on that
// target runs, a control being defined by which release it runs, because there
// is one control per production target the release has reached and the deploy
// record names each.
//
// A deploy names every schema change its build carries, and a revert's deploy is
// the one that carries more than one. [Writer.MarkSchemaChangesComplete] runs on
// the deploy that applied them, on the one that applied none because the store's
// history already held every one, and on an adoption's, so the record of a
// change that failed to apply is the only one naming changes that did not
// complete.
//
// Three fleets run on each target of a production deploy and the record dates
// each: [Fleets] holds the release's own, the control's, and the instances the
// build being replaced had, or the fraction of them an owner authored, kept
// until the last window that could return to it closes, each closed by one of
// the three teardown writes, and
// [Target.InstanceHours] is the three added up.
//
// The copy taken before a change that destroys stored data is deleted by the
// deployer: [DeleteExpiredSnapshots] is its own pass at the end of the retention
// the service record authors, and [DeleteSnapshot] is the one call both that
// pass and an owner at Ops make — the copy through the seam first, the deletion
// on the record after, so no record says a copy is gone while it can still be
// read.
//
// A backfill's record completes once its copy has: [Writer.Complete] refuses one
// whose [Backfill.Copied] is not written, which is what makes a complete record
// naming the element the fact enforcement reads.
//
// # A rollback is this record and not another
//
// [ShiftBack] is the fast rollback: a deploy event and not a version event, it
// shifts traffic onto the instances of the release it returns to — still running
// at full capacity, because the deploy that replaced that release kept them —
// and writes the same record, minting no number. It puts nothing on a target and
// verifies no artifact, the build being already there, and a target keeping
// nothing is [ErrNothingKeptToReturnTo] and [Restore]'s.
//
// [Restore] is the slow rollback: a deploy of the release being returned to,
// naming on the same record the release it failed, the releases it skipped,
// and the source that called for it — [Undoing]. It advances the deploys it
// undoes, [Performance.UndoneDeployIDs], one target at a time as it completes on
// each, so a rollback that stopped undoes nothing beyond the targets it reached. A record of its own was
// refused because a second writer on the fact of what is running is the fact
// the drift detector exists to check. Where a control kept the earlier release
// running, the fast rollback is a traffic shift and not this path at all;
// [Restore] is what a rollout without a control, or a control's own target,
// leaves. It verifies the artifact the host holds now against the digest the
// build record holds, before deploying anything and before asking any target
// what it runs, so a redeploy by name never restores bytes other than the ones
// that were verified. It applies no schema change and refuses a [Restoration]
// naming one with [ErrSchemaChangeAtARollback].
//
// [Resume] is the restart. It asks each target of a record it stopped in the
// middle of which build that target is running, through [Reading], and a target
// running the record's build with its row not complete is completed from that
// reading — the write is keyed on the record and the target, so a repeat writes
// nothing and no target is deployed to twice. It has two dispositions and not
// three, both inside the package: it completes a record every target the
// service runs on finished, and, for a record with something reached and
// something owed, either finishes it in the target order or returns every
// target it reached to the release that was current on it before — the fast
// way where the reached targets' kept fleet stands, the slow way otherwise —
// which of the two [Rebuilding], the caller's own seam, decides. A record no
// target reached, or one [Rebuilding] can carry neither way, is returned
// unchanged for the caller to carry on with. The one case it cannot decide is a
// schema change the record does not mark complete, which it marks failed at
// [StepSchemaChangeNotComplete] with the previous release left current, on the
// store rule. It reads only the started records, a failed one being left alone.
//
// [Mitigation] is a record and composes the columns every record table does.
// It is not a deploy: it is what Ops asks the deployer to perform on a target
// outside a rollout, on a human's instruction, and the drift detector reads a
// standing one as intended state that differs from the deploy record on
// purpose. It stands until a human ends it at Ops, and [Writer.EndMitigation]
// writes which human that was beside when they did, refusing anyone else with
// [ErrNotAHuman]. [Operations] holds two and not three — ending every instance of a
// service on a target is a third operation of the seam, which [Remove]
// performs for a retirement and no human at Ops instructs. [Writer] writes all
// three tables and nothing else writes any of them.
//
// # What the deployer writes elsewhere
//
// [Adopt] writes the service record's five reachability fields at adoption
// and at every first release, through that package's own writer inside this
// package's transaction — the service record has three writers and the field
// is the seam between them. [RecordTargetCheck] writes the deployer's last
// check over one target of a persistent environment, and the caller assembles
// [Found] from what the deploy just did; the two inputs this package cannot
// see are the emission the health monitor reads and whether it already
// carries traffic, both behind an interface of its own and doc.go says which
// caller supplies them.
//
// The deployer's last check per platform is lastcheck.Writer.RecordPlatformPass
// and not here: it is the sole writer of that record, keyed by the production
// environment record that declares the platform and not by the platform's own
// name, composing the payload from the three counts the design names rather
// than taking it as text, and the command-line interface's composition calls
// it beside [RecordTargetCheck] on every production deploy.
//
// # What is not built yet
//
// [Performance.Bake] takes an interface nothing implements: the health monitor
// is what could answer it, and a rollout given none holds nowhere between
// targets. [Performance.BakeVolume] is the value of the service record's own
// field, which the caller reads and supplies here — this package reaches no
// service record for a parameter, and a rollout with no [Performance.Bake] to
// ask holds nowhere whatever the volume is.
// way_in_token_digest is written at every deploy and read by
// [ByWayInTokenDigest], which is the report store's resolution of a
// submission's token to the deploy that placed the way in. The token itself is
// handed to the service in its configuration, under [WayInTokenName] and among
// the values the configuration digest is over, beside
// [Performance.WayInAddress] — the entrance the way in presents it at, which
// this package carries and never reads.
// [Mitigating.Principal] is the deployer's
// own principal, supplied by the caller — the command-line interface, until
// Ops is a screen.
//
// Who may write what: [Writer] inserts a deploy, its targets, and a
// mitigation, and advances each in place; nothing updates any other field and
// nothing deletes.
//
// What defines it: the deploy record in
// ../../end-goal/how-the-factory-works/06-releases/05-the-deploy-record/README.md
// (C1694, C1696, C1697, C1698, C1699, C1700, C1701, C1702, C1703, C1704, C1705,
// C1706, C1707, C1710, C1711, C1712, C1714, C1716, C1717, C1718, C1719, C1720,
// C1721, C1722, C1723, C1725, C1726) — written by the deployer through seam 4,
// advancing to complete or failed, keyed by service and environment; the
// strategy table in
// ../../end-goal/how-the-factory-works/03-gates/02-the-rollout-strategy.md
// (C0910, C0911, C0913, C0914, C0923, C0924, C0925, C0930, C0933, C0935,
// C0937, C0938, C0939);
//
// what a rollback is and what its record names, in
// ../../end-goal/how-the-factory-works/06-releases/06-rollback.md (C1727,
// C1728, C1729, C1730, C1731, C1732, C1734, C1736, C1737, C1738, C1753), and
// which release the slow one returns to, in
// ../../end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md
// (C2053, C2054, C2071), computed by the health monitor, which is what calls
// [Restore]; the restart and the deployer's write order in
// ../../end-goal/one-process.md (C2746, C2751, C2752, C2761); and the
// mitigation, which is a class of two operations, in ../../end-goal/deferred.md
// (C0096, C0103, C0105, C0108, C0109, C0119); the removal a retirement calls
// for, in
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/04-retirement.md
// (C0700, C0701, C0703). The three fleets, their spans and the instance-hour
// rate they are converted at are
// ../../end-goal/how-the-factory-works/06-releases/05-the-deploy-record/02-what-stands-for-a-rollback.md
// (C1681, C1682, C1684, C1685, C1686, C1687, C1688, C1690, C1691), the release a
// control runs is
// ../../end-goal/how-the-factory-works/06-releases/05-the-deploy-record/03-a-control-above-a-release.md
// (C1692), when a kept fleet is torn down and what bounds the batch a revert
// delivers are
// ../../end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md,
// and the schema history's row, the adoption's changes found applied, and the
// backfill the record marks complete are
// ../../end-goal/how-the-factory-works/07-contracts/09-the-store-is-a-contract-too.md
// (C1860, C1861, C1863, C1864, C1868, C1869, C1870, C1872, C1875).
//
// What adoption's deploy record and current release let the factory's
// checks read are
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/01-a-service-that-already-exists.md
// (C0628, C0629); being in an environment by having been deployed there is
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/README.md
// (C0722).
//
// The deploy record's naming of the control's build and instances, one control
// per production target reached, the deployer's read of its incomplete records
// at start, the restart completing or returning each target reached, the write
// keyed on deploy record and target, and an incomplete schema change marking
// the deploy failed are
// ../../end-goal/how-the-factory-works/08-operations/01-the-health-monitor.md
// (C1918, C1920, C1922, C1923, C1924, C1925).
//
// The record keyed by service and environment with completion per target,
// strategy performed stored beside strategy picked, one record per
// service and environment pair, are
// ../../end-goal/how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md
// (C1396, C1410, C1416); the schema step applying every change the build
// declares is
// ../../end-goal/how-the-factory-works/05-environments/02-an-environment-per-candidate/01-the-store-and-the-configuration.md
// (C1486); and strategy attaching to a production deploy and no other is
// ../../end-goal/how-the-factory-works/05-environments/04-what-the-candidate-environment-decides/README.md
// (C1593).
//
// The record naming every schema change the build carries, a revert's deploy
// carrying more than one, [Writer.MarkSchemaChangesComplete], and the two
// refusal errors at the schema and snapshot steps, are
// ../../end-goal/how-the-factory-works/06-releases/05-the-deploy-record/01-a-schema-change.md
// (C1663, C1665, C1666, C1667, C1671, C1675, C1676).
//
// [DeleteSnapshot] deleting the copy and writing the deletion on the record is
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/02-retention.md
// (C2306).
package deploy
