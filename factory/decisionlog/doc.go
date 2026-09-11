// Package decisionlog is the factory's one append-only log and the one writer
// that appends to it. Every component that decides anything calls [Writer]
// rather than writing into the table, which is the one-writer rule seam 2
// states. Reading the log goes through [Reader], which appends a read event of
// its own on every read, naming the principal that made the call.
//
// # The code
//
// row.go holds [Shape], one of [Shapes], [Part], [Formats] — which format
// version declares which shape — [Entry], which a caller supplies, [Row],
// which is what is stored, and [Row.ChainHash]. schema.go holds [Table],
// [Sequence], [IDPrefix], [AdvisoryLockKey], and [DDL]. writer_core.go holds
// [Writer], [NewWriter], and the transaction machinery every append method
// shares: taking the lease's fence and this package's advisory lock, reading
// the head, taking the next sequence value, validating [Entry.Principal]
// where the caller set one, hashing, and inserting.
// writer_decision.go holds the four decision methods —
// [Writer.AppendDecisionOpen], [Writer.AppendDecisionClose],
// [Writer.AppendDecisionAbandonment], [Writer.AppendDecisionAcknowledgement] —
// and the checks shared across more than one of them: [refuseClosingOnlyFields]
// and the actor-kind rule an abandonment and an acknowledgement each take.
// writer_wait.go holds [Writer.AppendWaitOpen] and [Writer.AppendWaitClose],
// the former refusing an actor that is not a component.
// writer_shapes.go holds the six one-row methods that name no version and
// close nothing: [Writer.AppendPageEvent], [Writer.AppendReworkRequest] —
// which admits [Entry.Reason] and [Entry.ReturnsTo], both required —
// [Writer.AppendQueueRejection] — which admits [Entry.Reading], required, and
// [Entry.MovedRelease], which may be empty — [Writer.AppendPolicyVersion],
// [Writer.AppendScoreVersion], [Writer.AppendInstallEvent], and the two
// appends a caller's own transaction holds — [Writer.AppendPolicyVersionInTx],
// package policy's, which writes the scope record's field in the same one, and
// [Writer.AppendPageEventInTx], package notifier's, which answers a wait at
// the same write the component that ends it makes. truncate.go holds
// [Cut] and [Writer.Truncate]. read.go holds [Reader], [NewReader], and
// [Reader.Read], [Reader.Verify], [Reader.ClosedDecisions], [Reader.Pending],
// [Reader.PendingWaits], [Reader.ClosedWaits], [Reader.ByShape], each of which
// takes a [principal.Principal] and appends a
// read event naming it before it answers — the actor's three fields in the
// row's own columns and the dispatch and the scope in the payload — and
// [Reader.AppendReadEvent], the same append for a reader of stored report text
// a redaction could reach, whose words are not in this log and whose store is
// not this one. verify.go holds the
// chain walk beneath [Reader.Verify], with [Break] and [BrokenError] naming
// the first row that breaks it. closed.go holds [Closed] and the pairing
// beneath [Reader.ClosedDecisions]; closedwait.go holds [ClosedWait] and the
// same pairing beneath [Reader.ClosedWaits].
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
//	returns_to, reading, moved_release
//	caller_kind, caller_key, caller_key_basis, caller_dispatch_id, caller_scope
//
// Each field is written as its length in bytes, big-endian in eight bytes,
// then the bytes themselves, so no two different rows serialise the same way.
// A future format version needing a different serialisation branches
// [Row.ChainHash] on the stored format version; today there is one branch.
//
// The close event's columns — verdict, reason, opened_in_work_at,
// self_approval, returns_to — sit beside the payload rather than inside it,
// because the writer enforces rules over them: a reject or a hold requires a
// reason, only a decision closing carries a verdict at all, and a non-empty
// opened_in_work_at requires the caller to be the Work screen
// ([ErrOpenedInWorkAtCaller]). This package reuses the reason column for a
// decision abandonment's own reason, the field the design calls "why no
// verdict is coming", and for a rework request's defect stated; it reuses the
// returns_to column for a rework request's own target, the field a reject's
// close event carries, handed the target the same way. None of the shapes
// that share a column occur on the same row — a row is either a closing, an
// abandonment, or a rework request, and never two of them — so one column
// serving all of them keeps the schema at one field rather than several that
// would only ever hold one value between them. [Entry.Reason] and
// [Entry.ReturnsTo] carry whichever the row's shape and part give them.
// reading and moved_release are the same shape of reuse, a queue rejection's
// own pair, and carry no other shape's field at all.
//
// The log's writer refuses five closes, and three of the five are checked
// here. [Writer.AppendDecisionClose] refuses a reject or a hold with no
// reason; it refuses a second close on one opening and a close on an opening
// an abandonment has ended, which are the design's second and third and are
// one check here, [ErrAlreadyEnded] over both endings — inside the
// transaction under the advisory lock, beside the unique indexes that refuse
// the same thing where a row reaches the store around this writer.
// [ErrNotAnOpening] stands beside the five and is none of them: it refuses a
// closing naming a row that is not a decision's opening at all.
//
// The other two of the five — a refer with nobody left to refer to, and a
// closing whose actor wrote the artifact version its opening names where
// another holder of the row's duty exists — depend on the People declaration
// and the artifact store, neither of which this package may import.
// [Writer.RefuseClose] is where the gate component supplies them: a function
// called inside the same transaction, after this package's own checks and
// before the insert, that may refuse the close for a reason of its own. A nil
// value refuses nothing extra.
//
// An acknowledgement takes the same [ErrAlreadyEnded] check, for a reason of
// its own rather than as a sixth refused close: it sits between the opening
// and the row that ends it, so one appended after a close or an abandonment
// would report a shared duty's time on a decision nobody was deciding.
//
// Three rows fix the kind of actor that may write them, over and above
// [record.Actor.Validate]'s own rule that a kind be one of [record.Kinds]:
// only a human acknowledges ([ErrAcknowledgementNotHuman]), only a component
// abandons ([ErrAbandonmentNotComponent]), and only a component opens a wait
// ([ErrWaitOpenNotComponent]) — the component that met the condition being
// the design's own actor for that row. A wait's closing takes no such rule:
// whichever component next reaches the work the wait stopped may write it,
// and that may not be the one that met the condition.
//
// [Entry.Principal] is who made the call, carried beside [Entry.Actor], who
// decided; the two are different facts and often the same value, and most
// callers — every one not yet composed for seam 5 — leave the zero value,
// which stores no caller at all. Where it is set it is validated the way
// [Reader]'s methods validate the principal a read names, and stored in the
// caller_kind, caller_key, caller_key_basis, caller_dispatch_id and
// caller_scope columns beside the actor's own. [Writer.AppendDecisionClose]
// is the one caller this package itself gives a rule to: a non-empty
// opened_in_work_at is refused unless the principal is the Work screen's own
// component principal, since the field is Work's report of when the human
// opened the row there and not the human's.
//
// [Writer.Truncate] appends a [Cut] as a truncation row and then deletes every
// row with a lower sequence, in one transaction under the lock and the fence.
// It takes the legal holds standing beside the cut and refuses the truncation
// where any of them does ([ErrLegalHoldStands]): a truncation is refused
// wherever a legal hold reaches, and the caller reads them because the package
// owning that record may not be imported here. It refuses a cut naming no
// retention value ([ErrNoRetentionInForce]) and one whose boundary is younger
// than the value reaches back ([ErrBoundaryInsideTheRetention]), so the row's
// claim about the value it enforced is one the cut obeyed.
// Which caller is not built: nothing enforces the retention value on a pass of
// its own. [Writer.Truncate] is the call enforcement makes, and its one caller
// is the command-line interface's `truncate`, which reads the value in force
// and who authored it, reads the legal holds standing, and cuts to the boundary
// a human named.
// [Writer.AppendReworkRequest] has no caller either: what writes one is
// whoever was authoring at the stage, through the component that dispatches,
// and nothing in the module makes that call yet. What it will carry is built
// here regardless: [Entry.Reason], the defect found, and [Entry.ReturnsTo],
// what owns it, both required.
//
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
// ../../end-goal/deferred.md#security-comes-last (C0037, C0038, C0042, C0043,
// C0044, C0045, C0046, C0050, C0051, C0053, C0054, C0055, C0056, C0057, C0058,
// C0059, C0060, C0061, C0062, C0063, C0064, C0065, C0066, C0067, C0068, C0071,
// C0073, C0075, C0076, C0077, C0079, C0082, C0083, C0110, C0111), which is also
// where the read event naming the principal is stated and where the principal
// on a call, carried on a write too, is seam 5. The four rows of a decision are
// ../../end-goal/how-the-factory-works/03-gates/01-where-a-gate-is-and-what-decides-it.md
// (C0838, C0839, C0840, C0847, C0848, C0850, C0851, C0852, C0853, C0859, C0860,
// C0861, C0862, C0867, C0872, C0873, C0874, C0878, C0881, C0893).
//
// The wait's two rows and the three kinds of hold are
// ../../end-goal/how-the-factory-works/03-gates/04-what-a-gate-may-change.md
// (C0965, C0974, C0975, C0976, C0977, C0978, C0979, C0980).
//
// The read event a reading of stored report text appends, which is what makes
// who had already read the words answerable after a redaction, is
// ../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/02-reports.md
// (C0444).
//
// A page event is
// ../../end-goal/how-the-factory-works/08-operations/07-pages.md (C2113). The
// install event, whose shape the merge queue also writes the numbers a mint
// after a restore passed over under, is
// ../../end-goal/how-the-factory-works/05-environments/05-what-the-queue-reads-before-it-mints.md
// (C1622).
//
// Truncation and decision-log retention are
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/02-retention.md
// (C2300, C2316, C2317, C2318), and a truncation refused wherever a legal hold
// reaches is
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/03-a-legal-hold.md
// (C2321, C2323).
//
// The fencing token and the head-conditioned append are
// ../../end-goal/one-process.md (C2746, C2748, C2749).
//
// The rework request row, its owner and defect, the reason field it shares
// with the reject, a backstopping human's row, and the log writing it with
// the author as actor are
// ../../end-goal/how-the-factory-works/03-gates/06-going-back-up.md (C1015,
// C1016, C1017, C1018).
//
// The wait's open row carrying the meeting component as actor, Pending being
// open with neither close nor abandonment, and the human's load being two
// reads of the log, are
// ../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md
// (C2644, C2645).
package decisionlog
