// Package halt owns the halt record: the one authored record whose subject is
// the factory, and its withdrawal — a second record naming it, never a field
// flipped in place.
//
// schema.go is [Table], [WithdrawalTable], the two id prefixes and [DDL].
// writer.go holds [Halt] and [Withdrawal] as they are stored; the tx-taking
// [Insert], [InsertWithdrawal] and [ApproveWithdrawal], meant to be called
// inside a caller's own transaction the way package policy calls a record
// package's write; [Writer] and [NewWriter], which wrap those three in their
// own transaction for a caller — a test, today — that holds none of its own;
// and [Standing], every halt with no approved withdrawal.
//
// A withdrawal is written pending and is not in force until a second write
// approves it, the way the gate row A halt's withdrawal decides one, held by
// a human always and routed to the owner, the halt's subject being the
// factory. Nothing here combines the two writes into one call the way package
// safeguard stands in for its own withdrawal's gate row: the gate row is
// gate.KindHaltWithdrawal, and what it calls at its close is package policy's
// ApproveHaltWithdrawal.
//
// Who may write what: [Writer] is Factory, and package policy's own writes —
// SetHalt, WriteHaltWithdrawal and ApproveHaltWithdrawal — call the tx-taking
// writes here and append the policy version each of them calls for in the same
// transaction. Nothing here fires the deploy to production hold or stops the
// merge queue's fast-forward that a standing halt calls for: [Standing] is what
// the gate component reads at that row and the merge queue at its stop, each
// applying the refusal or the stop itself.
//
// What defines it: the halt itself is
// ../../end-goal/how-the-factory-works/09-gate-policy/04-stopping-the-factory.md.
// Its withdrawal's gate row is
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/11-a-halts-withdrawal.md.
package halt
