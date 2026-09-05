// Package window owns the analysis window record — how long the factory may act
// on the health monitor alone, as a record rather than a timer — and the mark a
// named human at Ops writes against a rollback.
//
// # The files
//
// window.go is [Window], [Exit] with the four [Exits] and
// [Exit.PassedOrTimedOut], [Read] and [SeriesCounts], [Window.Open],
// [Window.Comparisons], [Window.Boundary], [Worse], [Window.PastCap], and the
// errors. opening.go is [OpenEvent] and how the per-quantity values and the
// lists of names are stored. writer.go is [Writer] and [NewWriter]:
// [Writer.Open] inserts, and [Writer.Close] writes exactly one exit with the
// [Closing] it closed on and refuses a second. read.go is the reads. mark.go is
// [Mark], [WriteMark], [Marked] and [Marks]. schema.go is [Table], [MarkTable],
// the id prefixes, the format versions, and [DDL].
//
// db_test.go is the tests against the database for [Writer.Open] and holds the
// fixtures the other test files use; close_test.go is [Writer.Close], the read
// an exit was decided on, and [Window.PastCap]; read_test.go is the tests for
// the reads and the mark. The three are one external test package split by
// subject, each file being held to 500 lines.
//
// # What a window carries
//
// A window names the deploy it was opened over and, through it, the release, the
// service, and the control; where the deploy is the search's, whose record names
// no release, it names the build alone, which is why [Window.ReleaseID] may be
// empty and its uniqueness is a partial index.
//
// It copies what was in force at the open rather than leaving it to be read back
// later: the size and the power per [gatepolicy.Quantity], the confidence, the
// cap, [Window.BoundaryVersion], the target set the boundary was allocated over,
// the operations read alone, the emission version each arm was read at with the
// quantities that were outside the set because the two differ, and the size and
// run length of the two other readings that could fail the release. An owner who
// re-authored a size while a window was open would otherwise change what a
// window already closed is read to have meant.
//
// [Window.PassedAvailable] is whether the passed exit was reachable at all and
// [Window.HeldOut] is the score's own sample running the release to the cap;
// both are the caller's to supply, and they are two fields because a window that
// ran to the cap for want of a control and one that ran to the cap for the
// sample are not the same window. [Writer.Close] refuses a passed close on a
// window that never had the exit. [Window.MeasuresNothing] is the window a
// service missing one of the four fields the deployer populates opens: it
// records only that, and [ErrMeasuresNothingCarriesNoParameters] refuses one
// that also names a parameter.
//
// [Window.ClosedOn] is the read the window closed on — the four counts per
// quantity and the same per target and operation — so an exit can be recomputed
// from the numbers it was decided on; the skipped exit carries none.
// [Window.FinestSizeReached] beside it is what the score reads: the size in
// force is the coarser of what the evidence asks for and what the traffic
// reached.
//
// The reads are [Get], [ForRelease], [ForDeploy], and [AllOpen]; [CountOpen],
// what the window limit is compared against, the limit itself being package
// policy's read; [ClosedPassedOrTimedOut] and [LastKnownGood], which is what a
// rollback's target and the last known-good release are computed from — the
// caller descending past a release whose deploy stopped before its build took
// traffic, which is the deploy record's fact and not this one's; [All]; and
// [Closed], every closed window of every service, which is what the score learns
// from. Ordering those windows by release number is the caller's, because this
// package does not read release records.
//
// # Who may write what
//
// [Writer] is the health monitor and there is no other. It inserts a window and
// closes it; nothing updates any other field and nothing deletes. [WriteMark] is
// Ops — it refuses an actor that is not a human, and so does the CHECK — and it
// takes the caller's transaction because the same event ends the revert item
// where the revert has not shipped. deploy_id, release_id, build_id, and
// service_id are id fields and not foreign keys, the rule record's doc.go states
// once.
//
// # What defines it
//
// The analysis window, its four exits, the parameters resolved at the open, the
// boundary version, and the power are
// ../../end-goal/how-the-factory-works/08-operations/02-the-analysis-window.md.
// The window limit, the last known-good release, a rollback's target, and the
// mark that a rollback was not caused by the release are
// ../../end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md.
// The quantities, the emission version, and the operation the series are kept
// per are
// ../../end-goal/how-the-factory-works/08-operations/01-the-health-monitor.md,
// and the four fields the deployer populates are
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/README.md.
// The boundary the size, the confidence and the power resolve to is package
// boundary.
package window
