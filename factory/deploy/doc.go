// Package deploy owns the deploy record and the rollout without a control: one
// record per rollout, written when the deploy starts, reaching the target through
// [targetseam.Target].
//
// # The record advances in place
//
// A deploy's status moves from started to complete, and from either to rolled
// back where a rollback undid the release. The record is an ordinary record and
// not a row of the log, and nothing chains it, so a status is an update where a
// gate's verdict is a second row; the design says so explicitly.
//
// Strategy has two values and only one is ever written. [StrategyWithControl] is
// in the CHECK because the record's definition names two strategies, and nothing
// writes it: serving a share of the traffic means deciding what fraction of
// arriving traffic reaches each of two builds, and a target that runs a release as
// a local process moves a process instead. So every deploy here goes without a
// control, which is the exemption a service's first release already takes arriving
// for the whole install, and it is the same value [StatusRolledBack] had for three milestones
// before anything wrote it.
//
// # A rollback is this record and not another
//
// [Restore] is the rollback: a deploy of the release being returned to, naming
// on the same record the release it condemned, the releases it swept, the
// source that called for it, and the intent it raised. A record of its own was
// refused for the reason the design gives — every field is on the deploy record
// already, and a second writer on the fact of what is running is the fact the
// independent checker exists to check. [Undoing.Any] is what tells a rollback's
// record from an ordinary deploy's.
//
// The condemned release stays a field apart from the swept ones, because one
// condemned release is one revert item and the swept ones were never condemned. The
// source is beside the actor and not instead of it: the actor is the deploy agent
// that performed the rollback, and the source is the health monitor at the condemned
// exit or
// the named human at Ops.
//
// Restore is the slow rollback, and on this substrate it is the only one. The fast
// path shifts traffic onto the control of the window immediately above the target,
// which is already running that build; a substrate that keeps no control has
// nothing to shift onto, so the build is started from cold and production runs the
// condemned release until it is up.
//
// One thing the design says of a condemned release is not true here: that it
// "never completes its deploy and so never becomes current". A deploy without a
// control has no schedule to abort — the process is started and the record completes, and only
// then does the window open and later cross — so on this substrate a condemned
// release was current for the length of its window. The design's own answer is
// what makes that harmless: what the window names is the release watched rather
// than the release current, exactly so the two need not agree.
//
// # What the record names as deployed
//
// [What] is the pair: the build the deploy put on the target, on every record,
// and the release the deploy is of, on every record but a candidate's. The build
// is there because the build is what runs — a release is the name a build has on
// master, which is a fact of this store and not of the target, so a target reports
// the build it is running and the build is what makes what runs comparable to what
// the record says. The release is absent on a deploy into a candidate's own
// environment, that deploy happening one gate before the merge that mints the
// number.
//
// # Current is what is running
//
// The record is keyed by service and environment and not by target, so a
// service's current release is single-valued per environment: the one
// [Current] names, the most recently completed deploy — what is running, not
// what is newest. A release minted and never deployed, and a deploy started
// and not completed, change nothing about the answer, which is why merged and
// running are different facts.
//
// # What a failed rollout leaves
//
// [WithoutControl] writes the record started, calls the target, and advances the
// record complete. On a target error the record stays started: the call may
// have failed before, during, or after the target acted, so writing either
// outcome would be a guess. The record saying started about a target that may
// or may not run the release is exactly the disagreement the independent checker reads
// targets to raise. [Restore] leaves the same thing on the same terms, and
// leaves nothing marked undone with it: a store saying a release was rolled back
// with nothing put back in its place would describe a service running nothing.
//
// Who may write what: [Writer] inserts a deploy and advances its status;
// nothing updates any other field and nothing deletes. service_id,
// environment_id, release_id, build_id, and a rollback's four are id fields and
// not foreign keys — a cross-package link is a field the link walk reads, and the
// store checks an id for being present and not for pointing at anything.
//
// What defines it: the deploy record in
// ../../end-goal/how-humans-do-it/06-releases.md#the-deploy-record — written
// by the agent performing the deploy through seam 4, advancing to complete or
// rolled back, keyed by service and environment — and the strategy table in
// ../../end-goal/how-humans-do-it/03-gates.md#the-rollout-strategy, whose two
// rows differ on whether the build being replaced is still serving; a
// service's first release goes without a control whatever the score would prefer,
// and on a substrate that moves a process rather than traffic every release does. What a
// rollback is and what its record names is
// ../../end-goal/how-humans-do-it/06-releases.md#rollback, and which release it
// returns to is
// ../../end-goal/how-humans-do-it/08-operations.md#overlapping-windows —
// computed for one rollback by the health monitor, which is what calls [Restore].
package deploy
