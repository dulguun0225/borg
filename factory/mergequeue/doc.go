// Package mergequeue is the merge queue: the one inbound path to master. It is
// a component and owns no table.
//
// # The files
//
// queue.go is [Actor], [AdvisoryLockKey], the errors, [Composition], [Queue],
// [New], [Outcome] and [Pass]. membership.go is [Membership], [Queue.Members]
// and the order. run.go is [Queue.Run]: the speculation, the merges in the
// queue's order, and the two comparisons no criterion reads. master.go is
// [Master] and the reading of master against the service's release records,
// with the completion of the queue's own unfinished merge. accept.go is
// [Queue.AcceptCommit] and [Acceptance]. mint.go is the mint, the two readings
// the number is taken from, and [SkippedNumbersPayload]. reading.go is
// [Reading], [Rejection], [RejectionPayload] and [Moved]. stop.go is [WaitKind],
// [WaitPayload] and the four conditions that stop a fast-forward. repository.go
// is the seams: [Repository], [Verified], [Confirmation], [Numbers],
// [DesignSystem] and [Backlog], each with the value a factory composed without
// it uses.
//
// The tests are fixtures_test.go and four files by subject: db_test.go for the
// two outcomes, the order and the lock; master_test.go for the readings of
// master, the acceptance and the number after a restore; stop_test.go for the
// halt, the backlog cap and the intent's state; reading_test.go for the three
// readings of a failure, the two comparisons and the speculation.
//
// # Who may write what
//
// The queue writes the release record through [release.Writer], the contract and
// its versions through [contract.Publish] inside that same transaction, and two
// kinds of row into the log through [decisionlog.Writer]: its rejection, and the
// waits its four stops stand as. It writes no item. The queue's row in
// ../../end-goal/components.md names the gate component, the build runner and
// the log and names no dispatch, so the transition each outcome causes — merged
// after a fast-forward, [Rejection.ReturnsTo] with an attempt counted there
// after a rejection — is the caller's write. That row and
// ../../end-goal/how-the-factory-works/03-gates/06-going-back-up.md disagree
// about it, and this package follows the row, components.md being where a call
// edge exists at all.
//
// Everything that touches the repository, the candidate's environment and the
// criteria is behind [Repository], implemented by whatever composes the deployer
// and the build runner. So the queue derives no contract form — [Verified]
// carries back the [contract.Form]s read off the re-verification, and the queue
// writes those.
//
// # What is not built
//
// Three readings the design gives the queue have no writer in the factory yet,
// so each arrives through the composition and says so on its type: the health
// monitor's store, which [Numbers] reads the second number from; the design
// system constraint records, which [DesignSystem] compares; and the backlog cap
// with what waits behind a rollback hold, which [Backlog] reads — the cap is a
// field of the service record beside the window limit and that field does not
// exist. mint.go states one departure of its own: the log holds ten shapes and
// none of them is a skipped-number row, so the numbers a mint passes over are
// written under the install event's shape with a payload kind of this package's.
//
// # What defines it
//
// ../../end-goal/how-the-factory-works/05-environments/03-the-merge-queue.md —
// the membership, the order, the speculation, the three readings of a failure,
// the design system comparison, and the queue being a component rather than a
// record;
// ../../end-goal/how-the-factory-works/05-environments/05-what-the-queue-reads-before-it-mints.md
// — master read against the records at every start and before every mint, the
// acceptance of a commit the queue did not make, and the number after a restore;
// ../../end-goal/how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md
// for the rejection written into the log and the comparison of the re-resolved
// set's digests;
// ../../end-goal/how-the-factory-works/06-releases/02-the-release-record.md for
// the queue as the release's writer;
// ../../end-goal/how-the-factory-works/06-releases/04-the-release-number.md for
// the number it mints with it;
// ../../end-goal/how-the-factory-works/07-contracts/01-two-versioned-things.md
// for the queue as the contract's writer, at the fast-forward of the first
// release that publishes it and in the same write as that release's first
// version;
// ../../end-goal/how-the-factory-works/09-gate-policy/04-stopping-the-factory.md
// for the halt's stop and the two candidates it passes;
// ../../end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md
// for the backlog cap's stop;
// ../../end-goal/how-the-factory-works/02-intent-into-items/02-the-interview.md
// for the intent's state, which permits membership or stops the item with a wait
// the queue opens and closes;
// ../../end-goal/how-the-factory-works/03-gates/06-going-back-up.md for the
// attempt being counted at what the item is sent to; and
// ../../end-goal/one-process.md for the restart, which is this queue reading
// master and writing the release record its own unfinished merge left owing.
package mergequeue
