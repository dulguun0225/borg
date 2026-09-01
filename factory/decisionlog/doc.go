// Package decisionlog is the factory's one append-only log and the one writer
// that appends to it. Every component that decides anything calls [Writer]
// rather than writing into the table, because five writers would be five
// implementations of the chain and five heads.
//
// # The three shapes
//
// A row is a decision, a page event, or a wait, and [Writer] has one method
// per kind of row rather than a shape argument. A page event and a wait are
// one row each. A decision is two rows: an open event appended when the gate
// fires, naming the policy version and the score version it was decided
// under, and a close event appended when the verdict is given, naming the
// open event it closes and neither version — what a decision was made under
// is a fact of the firing, written once where the firing is, so two rows
// cannot come to disagree about it. The verdict cannot be written onto the
// open event instead, because that row is chained the moment it is appended
// and a verdict added afterwards would be a rewrite of a chained record.
//
// Each rule is enforced twice. The methods refuse an entry that breaks it,
// and [DDL] refuses the same row again with a CHECK constraint, so a row
// inserted around the methods is refused too. [Writer.AppendDecisionClose]
// also checks, inside its transaction and before its insert, that the row it
// names exists and is an opening decision row; a second closing on one
// opening is refused by a partial unique index on closes. Every reader of the
// log filters on the shape it wants and joins a decision's two rows by the
// closing's closes field, which is what the shapes and the split cost.
//
// # The chain
//
// Each row names its predecessor's hash, and the first row names the empty
// string. The hash is SHA-256 over this serialisation, in this order:
//
//	the format tag "borg/factory/decisionlog/v2"
//	prev_hash
//	seq, in decimal
//	id
//	actor_kind
//	actor_name
//	at
//	shape
//	payload
//	policy_version
//	score_version
//	part
//	closes
//
// Each field is written as its length in bytes, big-endian in eight bytes,
// then the bytes themselves. Length-prefixing is what makes the serialisation
// unambiguous: there is no separator, so no field can contain one and no two
// different rows can serialise the same way. The format tag is first so that a
// later serialisation is a different chain rather than a silent
// reinterpretation of this one.
//
// Two column types follow from hashing stored bytes. The payload is text and
// not JSONB, because JSONB normalises what it stores — key order, whitespace,
// number spelling — and the chain hashes exact bytes. The timestamp is text in
// [record.TimeLayout] for the same reason, and the writer sets it rather than
// its caller.
//
// # What the chain proves, and what it does not
//
// [Verify] catches a row edited in place and a row removed, inserted, or
// reordered from inside the chain. It does not catch rows removed from the
// end: nothing anchors the head, so a truncated tail is not there to be
// checked, and an ordinary append then writes a replacement over it that
// verifies clean. So the chain proves that the rows present are one unbroken
// history and does not prove that they are the whole of it.
//
// That is seam 2's own sequencing and not a defect here — "Where the head is
// anchored and who reads it can wait",
// ../../end-goal/deferred.md#security-comes-last. What the seam says cannot
// wait is the field, which is here from the first record. Anchoring the head
// somewhere this package's writer cannot reach is what closes the rest, and
// [Verify] says so at the point a reader would rely on it.
//
// # Reading needs no writer
//
// [Verify] and [Read] are functions taking the pool, not methods on [Writer],
// so a component that reads the log or checks its health is not handed the
// thing that appends to it. [Writer] has the four append methods and nothing
// else.
//
// # One writer, enforced
//
// An append takes one PostgreSQL advisory lock for the whole transaction
// before it reads the head, so the one-writer rule is enforced by the database
// and not assumed of the callers. The transaction runs at read committed,
// stated explicitly rather than inherited: at repeatable read the snapshot is
// taken before the lock is granted, so the head read would be the head as of
// before the previous appender committed, and two rows would name the same
// predecessor.
//
// The lock is one key across the whole database and not one per schema, which
// is right for a self-hosted install whose factory has one database, and is
// what makes two logs in two schemas of one database serialise against each
// other. Nothing here is wrong when they do; they are slower.
//
// Row order is the seq column, taken from a sequence the writer advances
// itself. It is not a bigserial default because seq is one of the hashed
// fields and so has to be known before the row is written rather than returned
// after it. A transaction that rolls back has already consumed its sequence
// value, so seq has gaps and [Verify] does not require it to be contiguous.
// What it does require is that reading in seq order is reading in commit
// order, which holds because the lock is held across the transaction.
//
// Who may write what: this package inserts into decision_log and updates and
// deletes nothing. Append-only is a promise the writer makes about itself —
// nothing in the schema stops a superuser, which is what the chain is for.
//
// What defines it: seam 2 of "Security comes last" in
// ../../end-goal/deferred.md#security-comes-last, which sets the three shapes,
// the chain, and the one-writer rule. A page event is
// ../../end-goal/how-the-factory-works/08-operations/07-pages.md. The policy version a
// decision names is ../../end-goal/how-the-factory-works/09-gate-policy/README.md and the
// score version is
// ../../end-goal/how-the-factory-works/04-risk-score/01-factors-at-least.md. How long
// the log is kept is a setting an owner authors,
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it.md, and
// nothing here deletes anything yet. The two-row decision is
// ../../end-goal/how-the-factory-works/03-gates/01-where-a-gate-is-and-what-decides-it.md:
// the open event exists so the human has the factor vector to argue with,
// and the verdict is a second row because writing it onto a chained row would
// be a rewrite.
package decisionlog
