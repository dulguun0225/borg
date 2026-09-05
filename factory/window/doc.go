// Package window owns the analysis window record: how long the factory may act
// on the health monitor alone, as a record rather than a timer.
//
// window.go is [Window] and the [OpenEvent] it is created from, [Exit] with the
// four [Exits] and [Exit.Counts], [Window.Open], [Window.PastCap], and the
// errors. A window names the deploy it was opened over and through it the
// release and the service, and it copies the size, the confidence, the cap, the
// boundary's [Formula], and the policy and score versions in force at the open,
// rather than leaving them to be read back later: an owner who re-authors a size
// while a window is open would otherwise change what a window already closed is
// read to have meant. [Window.PassedAvailable] is whether the passed exit was
// reachable at all and [Window.HeldOut] is the score's own sample running the
// release to the cap; both are the caller's to supply, and they are two fields
// because a window that ran to the cap for want of a baseline and one that ran
// to the cap for the sample are not the same window. [Window.ClosedOn] is the
// [boundary.Observed] the window closed on, so an exit can be recomputed from
// the numbers it was decided on; the skipped exit carries none.
//
// writer.go is [Writer] and [NewWriter]: [Writer.Open] inserts, [Writer.Close]
// writes exactly one exit and refuses a second. schema.go is [Table],
// [IDPrefix], and [DDL].
//
// read.go is the reads. [Get], [ForRelease], [ForDeploy], and [AllOpen];
// [CountOpen], what the window limit is compared against, the limit itself being
// package policy's read; [ClosedWithoutFailing], every window of one service
// whose exit counts, which is what a last known-good release and a rollback's
// target are computed from; [All]; and [Closed], every closed window of every
// service, which is what the score learns from. Ordering those windows is the
// caller's, because the order is the release's number and this package does not
// read release records.
//
// db_test.go is the tests against the database for [Writer.Open] and
// [Writer.Close]; read_test.go is the tests for the reads, split out because
// the two together passed 500 lines.
//
// Who may write what: [Writer] is the health monitor and there is no other. It
// inserts a window and closes it; nothing updates any other field and nothing
// deletes. deploy_id, release_id, and service_id are id fields and not foreign
// keys, the rule record's doc.go states once.
//
// What defines it: the analysis window, its four exits, and the parameters resolved
// at the open are ../../end-goal/how-the-factory-works/08-operations/02-the-analysis-window.md;
// the window limit, the last known-good release, and a rollback's target are
// ../../end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md; and the
// boundary the size and confidence resolve to is package boundary.
package window
