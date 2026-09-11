// Package mergequeue is the merge queue: the one inbound path to master. It is
// a component and owns no table.
//
// # The files
//
// queue.go is [Actor], [AdvisoryLockKey], the errors, [Composition], [Queue],
// [New], [Outcome] and [Pass]. membership.go is [Membership] and
// [Queue.Members], the order, and the exclusion of an item that already holds a
// release. run.go is [Queue.Run]: the speculation, the merges in the queue's
// order, and the two comparisons no criterion reads. master.go is [Master],
// [Queue.readMaster] and [Queue.complete]: the one reading of master against the
// service's release records that the start, every mint, and a restart each
// make, against every build the records hold naming the service and the
// commit — [gate.ApprovalTimes] over [gate.MergeToMaster] is what tells a build
// the queue once approved from one it never did, whatever stage the item
// stands at now — with the completion of the queue's own unfinished merge,
// held as a wait rather than minted where the intent's state stops the item.
// accept.go is [Queue.AcceptCommit] and
// [Acceptance], which reads master the same way before its own mint and
// completes an unfinished merge the reading finds rather than minting blind.
// mint.go is the mint, the two readings the number is taken from — the second
// gated on an install event that says there was a restore — and
// [SkippedNumbersPayload]. reading.go is [Reading], [Rejection] with
// [Rejection.TeachesNothing], [RejectionPayload], [Moved] and [refuseIfRepeats],
// the check behind [ErrReverificationRepeats] that a re-verification deciding a
// candidate's own merit never names the environment cycle already in force —
// the build repeating is read as nothing to compare, master already an
// ancestor of the candidate branch being a no-op merge in git. stop.go is
// [WaitKind], [WaitPayload] and the four conditions that stop a fast-forward.
// repository.go is the seams: [Repository], [Verified], [Confirmation],
// [Numbers], [DesignSystem], [Backlog], [Reverts] and [Reliability], each with
// the value a factory composed without it uses.
//
// The tests are fixtures_test.go and six files by subject: db_test.go for the
// two outcomes, the order, the lock and the reliability reading; master_test.go
// for the readings of master; accept_test.go for [Queue.AcceptCommit], split out
// of master_test.go once the two together passed the line bound; mint_test.go
// for the number after a restore; stop_test.go for the halt, the backlog cap
// and the intent's state; reading_test.go for the three readings of a failure,
// the two comparisons and the speculation.
//
// # Who may write what
//
// The queue writes the release record through [release.Writer], the contract and
// its versions through [contract.Publish] inside that same transaction, two
// kinds of row into the log through [decisionlog.Writer] — its rejection, and
// the waits its four stops stand as — and, through [item.Dispatch.ReturnTo], the
// item's send-back on a rejection: the rejection is the queue's to act on, there
// being no gate firing for a caller to act on instead, and
// [Rejection.CountsAnAttempt] says on the row that the stage entered next counts
// an attempt, [Dispatch.ReturnTo] itself counting nothing. It writes no other
// item transition: the queue's row in ../../end-goal/components.md names the
// gate component, the build runner and the log and names no dispatch to item, so
// the merge advance to merged a fast-forward causes is still the caller's write.
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
// Nothing writes an install event yet either — the row that says there was a
// restore, at every upgrade and at every start after one. mint.go reads the log
// for one against the shape ../../end-goal/deferred.md gives it, so [Numbers] is
// read only at the first mint on a service since a restore; with no writer of
// that event, every mint reads the records alone until one exists.
//
// # What defines it
// ../../end-goal/how-the-factory-works/05-environments/03-the-merge-queue.md
// (C1530, C1531, C1532, C1533, C1534, C1535, C1536, C1537, C1538, C1539,
// C1540, C1541, C1542, C1543, C1544, C1545, C1546, C1547, C1548, C1549, C1550,
// C1553, C1554, C1555, C1556)
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
// (C1427, C1428, C1431, C1432, C1436, C1457) for the rejection written into
// the log and the comparison of the re-resolved set's digests;
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
//
// The first fast-forward making the head master, over an existing repository,
// is
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/01-a-service-that-already-exists.md
// (C0622); the first fast-forward creating master is
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/README.md
// (C0721); the queue re-verifying against the master it merges into is
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/07-merge-to-master.md
// (C1154).
//
// Master read for a commit the queue did not place is seam 2 of
// ../../end-goal/deferred.md (C0094); and the confirming run running after
// the gate approved is
// ../../end-goal/how-the-factory-works/05-environments/04-what-the-candidate-environment-decides/01-the-third-outcome.md
// (C1562).
package mergequeue
