// Package decisionlog is the factory's one append-only log and the one writer
// that appends to it. Every component that decides anything calls [Writer]
// rather than writing into the table, because five writers would be five
// implementations of the chain and five heads.
//
// # The code
//
// row.go holds [Shape] — one of [Shapes] — [Part], [Entry], which a caller
// supplies, [Row], which is what is stored, and [Row.ChainHash]. writer.go
// holds [Writer], [NewWriter], the four append methods —
// [Writer.AppendDecisionOpen], [Writer.AppendDecisionClose],
// [Writer.AppendPageEvent], [Writer.AppendWait] — and [Read]. verify.go holds
// [Verify], with [Break] and [BrokenError] naming the first row that breaks
// the chain. closed.go holds [Closed] and [ClosedDecisions], the read of a
// decision's two rows joined. schema.go holds [Table], [Sequence], [IDPrefix],
// [AdvisoryLockKey], and [DDL].
//
// A row is a decision, a page event, or a wait, and there is a method per kind
// rather than a shape argument. A page event and a wait are one row each; a
// decision is two, an open event naming the policy version and the score
// version, and a close event naming the open event it closes and neither
// version. Each rule the methods enforce is enforced again by a CHECK in
// [DDL], so a row inserted around them is refused too;
// [Writer.AppendDecisionClose] checks inside its transaction that the row it
// names exists and is an opening, and a partial unique index refuses a second
// closing on one opening.
//
// [Row.ChainHash] is SHA-256 over this serialisation, in this order:
//
//	the format tag "borg/factory/decisionlog/v2"
//	prev_hash, seq in decimal, id
//	actor_kind, actor_name, at
//	shape, payload
//	policy_version, score_version
//	part, closes
//
// Each field is written as its length in bytes, big-endian in eight bytes,
// then the bytes themselves, so no two different rows serialise the same way,
// and the tag is first so that a later serialisation is a different chain. The
// payload column is text and not JSONB and the timestamp is text in
// [record.TimeLayout], because the chain hashes stored bytes.
//
// [Verify] catches a row edited in place and a row removed, inserted, or
// reordered from inside the chain. It does not catch rows removed from the
// end, nothing anchoring the head, and it says so where a reader would rely on
// it. [Verify] and [Read] take the pool rather than being methods, so a
// component that reads the log is not handed the thing that appends to it.
//
// An append takes [AdvisoryLockKey] for the whole transaction before it reads
// the head, so one writer is enforced by the database, at read committed
// stated explicitly: at repeatable read the snapshot precedes the lock and two
// rows would name the same predecessor. Row order is seq, taken from
// [Sequence], which the writer advances itself because seq is hashed and so is
// known before the insert; a rolled-back transaction leaves a gap, so [Verify]
// requires seq order and not contiguity.
//
// Who may write what: this package inserts into decision_log and updates and
// deletes nothing. Append-only is a promise the writer makes about itself —
// nothing in the schema stops a superuser, which is what the chain is for.
//
// What defines it: the three shapes, the chain, the one-writer rule, and where
// the head would be anchored are seam 2 of "Security comes last",
// ../../end-goal/deferred.md#security-comes-last. The two-row decision is
// ../../end-goal/how-the-factory-works/03-gates/01-where-a-gate-is-and-what-decides-it.md.
// A page event is
// ../../end-goal/how-the-factory-works/08-operations/07-pages.md. The policy
// version a decision names is
// ../../end-goal/how-the-factory-works/09-gate-policy/README.md and the score
// version
// ../../end-goal/how-the-factory-works/04-risk-score/01-factors-at-least.md.
// How long the log is kept is a setting an owner authors,
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/README.md,
// and nothing here deletes anything.
package decisionlog
