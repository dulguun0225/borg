// Package decisionlog is the factory's one append-only log and the one writer
// that appends to it. Every component that decides anything calls [Writer]
// rather than writing into the table, because eleven writers would be eleven
// implementations of the chain and eleven heads. Reading the log goes through
// [Reader], which appends a read event of its own on every read, because
// reading evidence is itself something the log records.
//
// # The code
//
// row.go holds [Shape], one of [Shapes], [Part], [Formats] — which format
// version declares which shape — [Entry], which a caller supplies, [Row],
// which is what is stored, and [Row.ChainHash]. schema.go holds [Table],
// [Sequence], [IDPrefix], [AdvisoryLockKey], and [DDL]. writer_core.go holds
// [Writer], [NewWriter], and the transaction machinery every append method
// shares: taking the lease's fence and this package's advisory lock, reading
// the head, taking the next sequence value, hashing, and inserting.
// writer_decision.go holds the four decision methods —
// [Writer.AppendDecisionOpen], [Writer.AppendDecisionClose],
// [Writer.AppendDecisionAbandonment], [Writer.AppendDecisionAcknowledgement].
// writer_wait.go holds [Writer.AppendWaitOpen] and [Writer.AppendWaitClose].
// writer_shapes.go holds the six one-row methods that name no version and
// close nothing: [Writer.AppendPageEvent], [Writer.AppendReworkRequest],
// [Writer.AppendQueueRejection], [Writer.AppendPolicyVersion],
// [Writer.AppendScoreVersion], [Writer.AppendInstallEvent], and
// [Writer.AppendPolicyVersionInTx], the one append a caller's own
// transaction holds — package policy's, which writes the scope record's
// field in the same one. truncate.go holds
// [Cut] and [Writer.Truncate]. read.go holds [Reader], [NewReader], and
// [Reader.Read], [Reader.Verify], [Reader.ClosedDecisions], [Reader.Pending],
// [Reader.ByShape], each of which appends a read event before it answers. verify.go holds the
// chain walk beneath [Reader.Verify], with [Break] and [BrokenError] naming
// the first row that breaks it. closed.go holds [Closed] and the pairing
// beneath [Reader.ClosedDecisions].
//
// A row is one of ten shapes, and there is a method per shape and part rather
// than a shape argument on one method. A decision is four possible rows: an
// **opening** naming the policy version and the score version, a **closing**
// naming the opening it closes and the verdict, an **abandonment** naming the
// opening it ends and why no verdict is coming, and an **acknowledgement**
// naming the opening and the human who has it. A wait is two rows, an opening
// and a closing that names the opening it closes; the other eight shapes are
// one row each, with an empty part. Each rule the methods enforce is enforced
// again by a CHECK in [DDL], so a row inserted around them is refused too.
//
// The format version every row carries, in [record.Columns], is what declares
// its shape here: [Formats] maps each format version this package accepts to
// the [Shape] it serialises, a row whose format version names none is
// refused, and a `format_version_matches_shape` CHECK keeps the two
// consistent — TestFormatVersionsMatchDDL is what checks it. [Row.ChainHash]
// hashes the row's own stored format version in place of a package-wide
// constant, so a later format version changing the serialisation changes what
// it hashes and not what an earlier row already wrote. Every format version
// this package accepts today shares one serialisation and algorithm, SHA-256
// over this length-prefixed order:
//
//	format_version
//	prev_hash, seq in decimal, id
//	actor_kind, actor_key, actor_key_basis, at
//	shape, payload
//	policy_version, score_version
//	part, closes
//	verdict, reason, opened_in_work_at, self_approval
//
// Each field is written as its length in bytes, big-endian in eight bytes,
// then the bytes themselves, so no two different rows serialise the same way.
// A future format version needing a different serialisation branches
// [Row.ChainHash] on the stored format version; today there is one branch.
//
// The close event's columns — verdict, reason, opened_in_work_at,
// self_approval — sit beside the payload rather than inside it, because the
// writer enforces rules over them: a reject or a hold requires a reason, and
// only a decision closing carries a verdict at all. This package reuses the
// reason column for a decision abandonment's own reason, the field the design
// calls "why no verdict is coming": the two never occur on the same row, since
// a row is either a closing or an abandonment and never both, and one column
// serving both keeps the schema at one reason field rather than two that
// would only ever hold one value between them. [Entry.Reason] carries either.
//
// The log's writer refuses five closes. Three are checked here:
// [Writer.AppendDecisionClose] refuses a reject or a hold with no reason,
// refuses a closing on a row that is not a decision opening
// ([ErrNotAnOpening]), and refuses a second ending on one opening
// ([ErrAlreadyEnded]) — checked inside the transaction under the advisory
// lock, beside the unique indexes that refuse the same thing where a row
// reaches the store around this writer. The other two — a refer with nobody
// left to refer to, and a closing whose actor authored the artifact version
// its opening names where another holder exists — depend on the People
// declaration and the artifact store, neither of which this package may
// import. [Writer.RefuseClose] is where the gate component supplies them: a
// function called inside the same transaction, after this package's own
// checks and before the insert, that may refuse the close for a reason of its
// own. A nil value refuses nothing extra.
//
// [Writer.Truncate] appends a [Cut] as a truncation row and then deletes every
// row with a lower sequence, in one transaction under the lock and the fence.
// [Reader.Verify] treats the oldest remaining row as the chain's checkpoint: a
// truncation row anywhere in what remains naming that row as its boundary is
// what lets its prev_hash differ from empty without breaking the chain: what
// [BreakPredecessor] still requires everywhere else.
//
// Every write any component makes to the factory's own store carries the
// fencing token described in ../../end-goal/one-process.md, and [NewWriter]
// and [NewReader] both take one: every append calls [lease.Fence] inside its
// own transaction, before the insert, so a stalled instance's write is refused
// rather than landing beside whoever holds the lease now.
//
// An append takes [AdvisoryLockKey] for the whole transaction before it reads
// the head, so one writer is enforced by the database, at read committed
// stated explicitly: at repeatable read the snapshot precedes the lock and
// two rows would name the same predecessor. The row is written only where the
// head is still the one its chain field hashes over — the append reads the
// head and computes the hash inside the same locked transaction as the insert
// — which is the condition ../../end-goal/one-process.md states beyond the
// fencing token alone. Row order is seq, taken from [Sequence], which the
// writer advances itself because seq is hashed and so is known before the
// insert; a rolled-back transaction leaves a gap, so [Reader.Verify] requires
// seq order and not contiguity.
//
// Who may write what: this package inserts into decision_log and updates
// nothing; [Writer.Truncate] is the one method that deletes, and only rows a
// [Cut] names as older than its boundary. Append-only beyond that is a
// promise the writer makes about itself — nothing in the schema stops a
// superuser, which is what the chain is for. Where the head is anchored is
// the drift detector's own store, which records the head each pass and
// verifies the chain still holds it, extended and nothing else; that record
// is outside this package, seam 2 of "Security comes last" states it, and
// what a truncation costs the score's per-author priors is
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/02-retention.md.
//
// What defines it: the ten shapes, the chain, the one-writer rule, and the
// fencing token are seam 2 of "Security comes last",
// ../../end-goal/deferred.md#security-comes-last. The four rows of a decision
// are
// ../../end-goal/how-the-factory-works/03-gates/01-where-a-gate-is-and-what-decides-it.md.
// The wait's two rows and the three kinds of hold are
// ../../end-goal/how-the-factory-works/03-gates/04-what-a-gate-may-change.md.
// A page event is
// ../../end-goal/how-the-factory-works/08-operations/07-pages.md. Truncation
// and decision-log retention are
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/02-retention.md.
// The fencing token and the head-conditioned append are
// ../../end-goal/one-process.md.
package decisionlog
