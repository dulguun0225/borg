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
// transaction for a caller — a test, today — that holds none of its own; and
// [Reaching], whether a hold stands over one subject, a hold on the whole
// factory reaching every subject asked about.
//
// A withdrawal is written pending and is not in force until a second write
// approves it: it ends only at a gate row of its own, held by a human always
// and routed away from the human who wrote it, the treatment the gate row A
// safeguard's withdrawal already gets. That row is not built, so nothing here
// combines the two writes into one call — a caller wanting that composes
// [InsertWithdrawal] and [ApproveWithdrawal] itself.
//
// Who may write what: [Writer] is Factory. Nothing here refuses a truncation,
// a report's expiry, or a People mapping's deletion while a hold reaches
// them — [Reaching] is what such a caller will read, none of which is built.
// Nothing here appends a policy version either; setting a hold and
// withdrawing it each call for one, and package policy does not import this
// package yet.
//
// What defines it: the hold itself, what it suspends, and its withdrawal are
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/03-a-legal-hold.md.
package legalhold
