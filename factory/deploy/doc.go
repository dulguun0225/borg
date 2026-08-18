// Package deploy owns the deploy record and the straight rollout: one record
// per rollout, written when the deploy starts, reaching the target through
// [targetseam.Target].
//
// # The record advances in place
//
// A deploy's status moves from started to complete — or, from M4, to rolled
// back. The record is an ordinary record and not a row of the log, and
// nothing chains it, so a status is an update where a gate's verdict is a
// second row; the design says so explicitly. Strategy has one value,
// straight: with a control is M4's row, and widening the CHECK in [DDL] is
// that milestone's edit, a CHECK being a schema edit each time a value is
// added. Nothing in M1 writes rolled_back — the value is in the CHECK because
// the record's definition names the three statuses.
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
// [Straight] writes the record started, calls the target, and advances the
// record complete. On a target error the record stays started: the call may
// have failed before, during, or after the target acted, so writing either
// outcome would be a guess. The record saying started about a target that may
// or may not run the release is exactly the disagreement the reconciler
// reads targets to raise, and the reconciler is M4 — until then a stuck
// started record is found by a reader noticing it, not by the factory.
//
// Who may write what: [Writer] inserts a deploy and advances its status;
// nothing updates any other field and nothing deletes. service_id and
// release_id are id fields and not foreign keys — a cross-package link is a
// field the link walk reads, and the store checks an id for being present and
// not for pointing at anything.
//
// What defines it: the deploy record in
// ../../end-goal/how-humans-do-it/06-releases.md#the-deploy-record — written
// by the agent performing the deploy through seam 4, advancing to complete or
// rolled back, keyed by service and environment — and the strategy table in
// ../../end-goal/how-humans-do-it/03-gates.md#the-rollout-strategy, whose two
// rows differ on whether the build being replaced is still serving; a
// service's first release is straight whatever the score would prefer, and
// M1's change is a first release.
package deploy
