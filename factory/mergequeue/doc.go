// Package mergequeue is the merge queue: the one inbound path to master. It is
// a component and owns no table.
//
// queue.go is the whole of it. [Queue] and [New] compose it, [New] taking the
// fencing token every write and every read of the log goes through; [Queue.Members]
// is the membership — the items whose stage says Merge to master approved them and
// whose fast-forward has not happened, which is [item.StageQueued] — ordered by
// the item's priority and then the time of that approval, read through
// [gate.ApprovalTimes] with the queue's token and [Actor] as the reading
// principal.
// [Queue.Run] re-verifies each member against the master it will actually merge
// into and returns one [Outcome] per member; a rejection on the candidate's own
// merits is a [RejectionPayload] of [RejectionKind], appended to the log as
// [Actor] through [decisionlog.Writer.AppendQueueRejection].
//
// Everything that touches the repository, the candidate's environment, and the
// criteria is behind [Repository], implemented by whatever composes the deploy
// agent: the queue orders merges and reaches no deploy target. So the queue
// derives no contract form — [Verified] carries back the [contract.Form]s the
// deploy agent read off the re-verification, and the queue writes those.
//
// The fast-forward mints the release with it, and the contract versions that
// release publishes are written in the same transaction through
// [release.Writer.MintWith], so a number never stands without the versions it
// publishes. [Queue.Run] holds a session-level advisory lock per service across
// the fast-forward, the mint, and the item's advance, so two runs of one service
// cannot interleave and two candidates cannot read one number.
// [AdvisoryLockKey] derives that key from this package's own name and not from
// [release.Writer.Mint]'s — one lock held while the other is waited for on
// another connection would be a deadlock the pool could not resolve. A member
// that already has a release is finished rather than re-verified, so one merge
// never mints two numbers.
//
// Who may write what: the queue writes the release record through
// [release.Writer], the contract and its versions through [contract.Publish]
// inside that same transaction, the item's stage through [item.Dispatch], and
// one row into the log through [decisionlog.Writer].
//
// What defines it:
// ../../end-goal/how-the-factory-works/05-environments/03-the-merge-queue.md — the
// membership, the order, the re-verification, the rejection and where it goes, and
// the queue being a component rather than a record;
// ../../end-goal/how-the-factory-works/06-releases/02-the-release-record.md for the queue
// as the release's writer;
// ../../end-goal/how-the-factory-works/07-contracts/01-two-versioned-things.md for the
// queue as the contract's writer, at the fast-forward of the first release that
// publishes it and in the same write as that release's first version; and
// ../../end-goal/how-the-factory-works/03-gates/06-going-back-up.md for the attempt being
// counted at what the item is sent to.
package mergequeue
