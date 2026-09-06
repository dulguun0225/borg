// Package legalhold owns the legal hold record: what suspends retention's
// removals over a subject, and its withdrawal — a second record naming it,
// never a field flipped in place.
//
// schema.go is [Table], [WithdrawalTable], the two id prefixes and [DDL].
// writer.go holds [SubjectKind] and [SubjectKinds] — a service, a project, or
// the whole factory, the last naming no subject of its own — [Subject],
// [Hold] and [Withdrawal] as they are stored; the tx-taking [Insert],
// [InsertWithdrawal] and [ApproveWithdrawal], meant to be called inside a
// caller's own transaction the way package policy calls a record package's
// write; [Writer] and [NewWriter], which wrap those three in their own
// transaction for a caller — a test, today — that holds none of its own;
// [GetWithdrawal], one withdrawal by id, which the gate row that decides it
// reads before it fires, the actor on that record being the one human the row
// may not route to; and
// [Reaching], whether a hold stands over one subject, a hold on the whole
// factory reaching every subject asked about and a hold on a project reaching
// every service in it; and [Standing], every hold in force, which is what a
// truncation of the decision log is refused against.
//
// A withdrawal is written pending and is not in force until a second write
// approves it: it ends only at the gate row [gate.KindLegalHoldWithdrawal], held by a
// human always and routed away from the human who wrote it, the treatment the
// gate row A safeguard's withdrawal already gets. Nothing here combines the two
// writes into one call — the row is what sits between them.
//
// Who may write what: [Writer] is Factory, and package policy's own writes —
// SetLegalHold, WriteLegalHoldWithdrawal and ApproveLegalHoldWithdrawal — call
// the tx-taking writes here and append the policy version each of them calls
// for in the same transaction. The refusals a standing hold carries are the
// callers': the decision log's truncation takes [Standing] and refuses the cut
// where any stands, People refuses the deletion of a mapping a hold reaches
// through [Reaching], and a report's expiry and a redaction are not built.
//
// What defines it: the hold itself, what it suspends, and its withdrawal are
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/03-a-legal-hold.md.
package legalhold
