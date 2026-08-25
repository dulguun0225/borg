// Package driftdetector owns the two records the drift detector writes and the reads the
// factory makes of them. It is one process outside the pipeline that reads what is
// actually running on each production target and compares it against that service's
// current release.
//
// # Why there is a second store at all
//
// Every check the factory makes reads a record the factory wrote. A service's
// current release is what its production deploy record names, the health monitor
// measures against what the factory recorded, and an incident points at a deploy the
// same log describes. So a factory whose records are wrong reports itself healthy and
// nothing downstream of them contradicts it.
//
// One fact, one comparison, read-only. Drift detection has no deploy
// privilege and writes into a store of its own that no factory component may
// write, which is why this package brings its own [Open] and [Apply] rather
// than reaching for the factory's: the factory's schema applier knows every
// record package and is called at the start of every factory process, and a
// store the factory applies is a store the factory owns. The duplication is
// about twenty lines and it is the cheaper half of the trade.
//
// It is not true that the factory reads nothing back. A mismatch holds a gate
// and pages, and both require reading it. What the independence actually
// requires is narrower and is the rule: nothing the drift detector writes
// is evidence about the software. It cannot cause anything to be built,
// deployed, scored, approved, or measured, and the one thing it can do is stop.
//
// # Two records and three readers
//
// [Mismatch] is read by the gate component at the moment the production deploy
// gate fires — through an interface, which [Store] implements — and by the
// notifier. A mismatch remains until a human clears it at the independent
// driftdetector, even where a later comparison agrees, which is recorded on it as
// [Mismatch.LaterAgreements] so the human clearing has the evidence. Clearing
// it from the factory is refused by there being no method here that a factory
// component holds: it would make the factory a writer of the record that says
// the factory is wrong, and a deploy that did not complete is precisely the
// kind of bug that would clear it on retry.
//
// [LastCheck] is the last check per production target and per service,
// overwritten each pass. It exists because a check that silently stops is worse
// than the bug it catches: it is read so a stopped drift detector is
// visible rather than silent, and by the gate only where a safeguard sets a
// maximum age on it — which nothing does here, that safeguard binding a
// parameter gate policy's rows do not hold.
//
// Failing to reach a target is not a mismatch. A network blip would otherwise hold
// every production deploy, so an unreached target is a last check with
// [LastCheck.Reached] false and the reason on it, and no mismatch at all.
//
// # What it catches and what it does not
//
// The record that was wrong when it was written: a deploy that did not complete, an
// agent that recorded one that never happened, a target changed underneath. That is
// the common failure and it is a bug rather than malice. What it does not catch is a
// record rewritten afterwards; anchoring the log's chain head is what would.
//
// What it is not is a second opinion on whether the software is good. It compares
// one recorded fact to the world. One that acquired a judgment of its own would be
// the second path the design refuses, arriving indirectly — so nothing here reads a
// criterion, a score, or a boundary.
//
// # The exception for a running control
//
// A build running on a production target beside the current release is a
// mismatch only where no open window names it, as the release under watch or as
// the control that window's deploy record names. Otherwise the independent
// driftdetector would page on every rollout it sees. [Pass.Excused] is where a caller
// says so, and on a substrate that moves a process rather than traffic it is
// never set: one directory runs one process, so there is never a second build
// beside the current release to excuse. What that costs is that the one path
// the design writes this exception for is unexercised here, and the field is
// carried so that a substrate which does keep a control does not have to add
// it.
//
// Who may write what: [Writer] inserts a mismatch, records a later agreement on
// one, clears one, and overwrites the last check. No factory component holds a
// [Writer] — this package's writer is the drift detector's own process,
// and what the factory holds is [Store] and the read functions.
//
// What defines it:
// ../../end-goal/how-humans-do-it/08-operations.md#drift-detection — the one process,
// the two records, the three readers, what clearing requires, and what it catches —
// and ../../end-goal/how-humans-do-it/03-gates.md#deploy-to-production for the hold
// it sets, which is the one hold the factory cannot lift by gathering evidence.
package driftdetector
