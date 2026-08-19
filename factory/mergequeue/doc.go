// Package mergequeue is the merge queue: the one inbound path to master.
//
// It is a component and owns no table. Its membership is the items whose stage
// says Merge to master approved them and whose fast-forward has not happened,
// which is [item.StageQueued]; its order is the item's priority and then the time
// of that approval in the decision log; and the two outcomes that outlast a run
// are records already — a fast-forward writes the release record, and a failure
// on the candidate's own merits writes a row into the log.
//
// # Why it exists
//
// Candidates verified in parallel cannot all fast-forward, because each was built
// on a master that has since moved. So a candidate entering the queue re-verifies
// against the master it will actually merge into and fast-forwards only if that
// passes. What that restores is the property one shared environment used to
// provide: the commit that was verified is the commit that merges, which is
// structural here rather than a discipline anyone has to keep.
//
// # It mints the release
//
// Master's only inbound path is this queue, so the fast-forward is the event and
// the queue is what performs it — and the release record and its number are minted
// with it. A writer of its own, called at the fast-forward, would be a component
// with one caller and the per-service ordering implemented again inside it.
//
// The fast-forward, the mint, and the item's advance to merged are three writes
// across a repository and two transactions, and nothing can make them atomic.
// [Queue.Run] holds a session-level advisory lock per service across all of them, so
// two runs of one service cannot interleave and two candidates cannot read one
// number. The order is the safer of the three: a release numbering a commit that
// never reached master is worse than a commit no release names, because a deploy
// could put the first one live.
//
// Two of the three windows are closed here and one is not. A member that already has
// a release is finished rather than re-verified, so an advance that failed is
// repaired on the next run and one merge never mints two numbers — [Queue.one] says
// how. A mint that failed after the fast-forward leaves master at a commit no
// release names, and that one is open: what repairs a record disagreeing with what is
// there is the reconciler, which is M4.
//
// The lock key is derived from this package's own name, so it is not the key
// [release.Writer.Mint] takes — one lock held while the other is waited for on
// another connection would be a deadlock the pool could not resolve.
//
// What the lock does cost is a connection held for the length of a run while every
// write inside it takes a second one from the same pool. One run per process is what
// the crude interface does and what this is written for; enough concurrent runs to
// exhaust the pool would each hold one connection and wait for another, and nothing
// would break that cycle. Closing it needs a connection limit the pool does not
// carry today, and it is named here because the per-service key is written for
// exactly the caller that would meet it.
//
// # What is not built
//
// The speculation. The design has a candidate re-verify against master plus every
// candidate ahead of it, which is what makes a long queue fast; [Queue.Run] verifies
// serially against the master that actually resulted. Those speculative
// re-verifications are the queue's own state, read by nothing outside it, so
// building them later changes no record — and what a serial queue already provides
// is the property the design asks of the queue. What it costs is wall-clock: a queue
// of ten waits ten re-verifications where a speculating queue overlaps them.
//
// Because nothing is speculated, nothing behind a failure is invalidated: a
// candidate after a rejection re-verifies against the same master it would have
// anyway, and counts nothing, which is what the design says of a candidate that
// failed because of somebody else.
//
// # Who may write what
//
// The queue writes the release record through [release.Writer], the item's stage
// through [item.Dispatch], and one row into the log through [decisionlog.Writer].
// Everything that touches the repository, the candidate's environment, and the
// criteria is behind [Repository], which is implemented by whatever composes the
// deploy agent: the queue orders merges and does not reach a deploy target.
//
// What defines it:
// ../../end-goal/how-humans-do-it/05-environments.md#the-merge-queue — the
// membership, the order, the re-verification, the rejection and where it goes, and
// the queue being a component rather than a record;
// ../../end-goal/how-humans-do-it/06-releases.md#the-release-record for the queue
// as the release's writer; and
// ../../end-goal/how-humans-do-it/03-gates.md#going-back-up for the attempt being
// counted at what the item is sent to.
package mergequeue
