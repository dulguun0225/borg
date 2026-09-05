// Package driftdetector owns the drift detector's own store: the mismatch,
// the recorded chain head, the last check per production target, and the
// detector's own address and delivery. It is one process outside the
// pipeline that reads what is actually running on each production target
// and the factory's own log and last checks, and compares them against what
// the factory recorded.
//
// # The files
//
// driftdetector.go is [Actor], [Mismatch] with [Mismatch.Cleared] and
// [Mismatch.Why], [HoldWords], [MismatchKindTarget] and [MismatchKindChain],
// [LastCheck] with [LastCheck.Stale], [Pass] with [Pass.Agreed], [Recorded],
// and [Writer] and [NewWriter] with [Writer.Record], [Writer.Clear] and
// [Writer.RaiseChainMismatch]. head.go is [Head], [Writer.RecordHead],
// [GetHead] and [VerifyChain], the second comparison's own. delivery.go is
// [Writer.SetAddress], [Address], [OwnDelivery], [Writer.Deliver] and
// [OwnDeliveries], the detector's own page. read.go is [Store] and
// [NewStore] with [Store.Mismatch], and the reads [Uncleared],
// [UnclearedChain], [All], [Get] and [LastChecks]. schema.go is every
// table's name and id prefix, [DDL], and this store's own [DefaultURL],
// [URLEnv], [URL], [Open] and [Apply].
//
// The tests are db_test.go, every one of them against the database.
//
// This package brings its own [Open], [Apply], [URL], and [DDL] rather than
// reaching for package postgres: the factory's schema applier knows every
// record package and is called at the start of every factory process, and a
// store the factory applies is a store the factory owns. The duplication is
// about twenty lines.
//
// A [Pass] is the first comparison — the target's build and, where it
// answers, its digest, against the release the factory recorded — and
// [Writer.Record] is what writes it: a [Mismatch] naming the service and the
// target where they disagree, and a [LastCheck] per production target either
// way, overwritten each pass. Failing to reach a target is not a mismatch —
// it is a last check with [LastCheck.Reached] false and the reason on it.
// [Pass.Excused] is where a caller says a build running beside the current
// release is a window's control; on a platform that moves a process rather
// than traffic it is never set, one directory running one process, and the
// field is carried so that a platform which does keep a control does not
// have to add it.
//
// [VerifyChain] is the second comparison: it reads the factory's log past
// the head this store recorded last pass and confirms the chain still holds
// it, extended and nothing else, using [decisionlog.Row] and
// [decisionlog.Row.ChainHash] alone — this package selects decision_log
// directly rather than importing [decisionlog.Reader], which would append a
// read event with a fencing token this package does not hold. Finding the
// chain broken is [Writer.RaiseChainMismatch]'s own mismatch, naming
// neither service nor target because it holds every service's production
// deploys at once — [MismatchKindChain] beside the ordinary
// [MismatchKindTarget].
//
// A mismatch remains until a human clears it through [Writer.Clear], even where
// a later comparison agrees, which is recorded on it as
// [Mismatch.LaterAgreements] so the human clearing has the evidence. There is
// no method here a factory component holds that clears one.
//
// [Store] is the gate component's read at the production deploy row, through an
// interface the gate declares, and [HoldWords] is what that hold says.
// [Uncleared], [UnclearedChain], [All], [Get], and [LastChecks] are the other
// reads, the notifier's among them: the notifier reads this store itself for
// the one wait that has no other caller, and reads [OwnDeliveries] to catch
// up the page event a delivery made while the factory's process was down.
//
// Nothing here reads a criterion, a score, or a boundary: nothing this package
// writes is evidence about the software, it can cause nothing to be built,
// deployed, scored, approved, or measured, and the one thing it can do is stop.
//
// Who may write what: [Writer] inserts a mismatch, records a later
// agreement on one, clears one, records the chain head, and overwrites the
// last check; [Writer.SetAddress] and [Writer.Deliver] are the detector's
// own page. No factory component holds a [Writer] — this package's writer
// is the drift detector's own process, and what the factory holds is
// [Store] and the read functions, including a read of the factory's own log
// and last-check tables that this package makes with a query of its own,
// never through their writers.
//
// What defines it:
// ../../end-goal/how-the-factory-works/08-operations/08-drift-detection.md — the one process,
// the two records, the four readers, the six comparisons, the detector's own
// delivery, and what clearing requires — and
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/08-deploy-to-production.md for the hold
// it sets, which is the one hold the factory cannot lift by gathering evidence.
package driftdetector
