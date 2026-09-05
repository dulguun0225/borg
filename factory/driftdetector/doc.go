// Package driftdetector owns the two records the drift detector writes and the
// reads the factory makes of them. It is one process outside the pipeline that
// reads what is actually running on each production target and compares it
// against that service's current release.
//
// # The files
//
// driftdetector.go is [Actor], [Mismatch] with [Mismatch.Cleared] and
// [Mismatch.Why], [HoldWords], [LastCheck], [Pass] with [Pass.Agreed],
// [Recorded], and [Writer] and [NewWriter] with [Writer.Record] and
// [Writer.Clear]. read.go is [Store] and [NewStore] with [Store.Mismatch], and
// the reads [Uncleared], [All], [Get] and [LastChecks]. schema.go is
// [MismatchTable] and [LastCheckTable] with their id prefixes, [DDL], and this
// store's own [DefaultURL], [URLEnv], [URL], [Open] and [Apply].
//
// The tests are db_test.go, every one of them against the database.
//
// This package brings its own [Open], [Apply], [URL], and [DDL] rather than
// reaching for package postgres: the factory's schema applier knows every
// record package and is called at the start of every factory process, and a
// store the factory applies is a store the factory owns. The duplication is
// about twenty lines.
//
// A [Pass] is one comparison and [Writer.Record] is what writes it: a
// [Mismatch] where what runs disagrees with the record, and a [LastCheck] per
// production target and per service either way, overwritten each pass. Failing
// to reach a target is not a mismatch — it is a last check with
// [LastCheck.Reached] false and the reason on it. [Pass.Excused] is where a
// caller says a build running beside the current release is a window's control;
// on a substrate that moves a process rather than traffic it is never set, one
// directory running one process, and the field is carried so that a substrate
// which does keep a control does not have to add it.
//
// A mismatch remains until a human clears it through [Writer.Clear], even where
// a later comparison agrees, which is recorded on it as
// [Mismatch.LaterAgreements] so the human clearing has the evidence. There is
// no method here a factory component holds that clears one.
//
// [Store] is the gate component's read at the production deploy row, through an
// interface the gate declares, and [HoldWords] is what that hold says.
// [Uncleared], [All], [Get], and [LastChecks] are the other reads, the
// notifier's among them.
//
// Nothing here reads a criterion, a score, or a boundary: nothing this package
// writes is evidence about the software, it can cause nothing to be built,
// deployed, scored, approved, or measured, and the one thing it can do is stop.
//
// Who may write what: [Writer] inserts a mismatch, records a later agreement on
// one, clears one, and overwrites the last check. No factory component holds a
// [Writer] — this package's writer is the drift detector's own process,
// and what the factory holds is [Store] and the read functions.
//
// What defines it:
// ../../end-goal/how-the-factory-works/08-operations/08-drift-detection.md — the one process,
// the two records, the three readers, what clearing requires, and what it catches —
// and ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/08-deploy-to-production.md for the hold
// it sets, which is the one hold the factory cannot lift by gathering evidence.
package driftdetector
