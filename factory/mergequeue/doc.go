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
// [DesignSystem], [Backlog] and [Reverts], each with the value a factory
// composed without it uses.
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
// about it, and this package follows the row, ../../end-goal/components.md being where a call
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
// Four readings the design gives the queue have no writer in the factory yet,
// so each arrives through the composition and says so on its type: the health
// monitor's store, which [Numbers] reads the second number from; the design
// system constraint records, which [DesignSystem] compares; what waits behind a
// rollback hold, which [Backlog] reads — the cap in force arrives with the
// count rather than being read here, the walk that produces the count being one
// the queue does not make; and which item is a revert, which [Reverts] answers — nothing on the
// item says it is one, and the record that links a revert a named human at Ops
// asked for to the release it undoes does not exist.
// mint.go states one departure of its own: the log holds ten shapes and
// none of them is a skipped-number row, so the numbers a mint passes over are
// written under the install event's shape with a payload kind of this package's.
//
// # What defines it
// ../../end-goal/how-the-factory-works/05-environments/03-the-merge-queue.md
// (C1530, C1531, C1532, C1534, C1536, C1537, C1538, C1540, C1541, C1542, C1543,
// C1544, C1545, C1546, C1547, C1548, C1549, C1550, C1553, C1554, C1555, C1556)
// — the membership, the order, the speculation, the three readings of a
// failure, the design system comparison, and the queue being a component rather
// than a record;
//
// ../../end-goal/how-the-factory-works/05-environments/05-what-the-queue-reads-before-it-mints.md
// (C1602, C1603, C1604, C1605, C1606, C1607, C1608, C1609, C1611, C1612, C1613,
// C1614, C1616, C1617, C1618, C1621, C1622, C1623) — master read against the
// records at every start and before every mint, the acceptance of a commit the
// queue did not make, and the number after a restore;
//
// ../../end-goal/how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md
// (C1427, C1428, C1431, C1436, C1457) for the rejection written into the log
// and the comparison of the re-resolved set's digests;
//
// ../../end-goal/how-the-factory-works/06-releases/02-the-release-record.md
// (C1639, C1640, C1641) for the queue as the release's writer;
//
// ../../end-goal/how-the-factory-works/06-releases/04-the-release-number.md
// (C1651, C1657, C1659, C1660, C1661, C1662) for the number it mints with it;
//
// ../../end-goal/how-the-factory-works/07-contracts/01-two-versioned-things.md
// (C1765) for the queue as the contract's writer, at the fast-forward of the
// first release that publishes it and in the same write as that release's first
// version;
//
// ../../end-goal/how-the-factory-works/09-gate-policy/04-stopping-the-factory.md
// (C2337, C2338, C2341, C2343, C2349) for the halt's stop and the two
// candidates it passes;
//
// ../../end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md
// (C2066) for the backlog cap's stop;
//
// ../../end-goal/how-the-factory-works/02-intent-into-items/02-the-interview.md
// (C0576, C0578) for the intent's state, which permits membership or stops the
// item with a wait the queue opens and closes;
//
// ../../end-goal/how-the-factory-works/03-gates/06-going-back-up.md (C1006,
// C1010) for the attempt being counted at what the item is sent to; and
// ../../end-goal/one-process.md (C2745, C2756, C2760) for the restart, which is
// [Queue.Restart]: this queue reading master and writing the release record its
// own unfinished merge left owing.
//
// Also ../../end-goal/components.md (C0013).
package mergequeue
