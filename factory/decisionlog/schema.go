package decisionlog

import "github.com/dulguun0225/borg/factory/record"

// Table is the one table this package owns.
const Table = "decision_log"

// Sequence is where seq comes from. It is declared rather than left to a
// bigserial default because the writer takes the next value itself: seq is
// hashed, so the row's hash cannot be computed until the value is known.
const Sequence = "decision_log_seq"

// IDPrefix is what [record.NewID] is called with for a row of this table.
const IDPrefix = "dl"

// AdvisoryLockKey is the PostgreSQL advisory lock every append takes for the
// whole of its transaction. It is the first eight bytes of
// SHA-256("borg/factory/decisionlog"), big-endian, with the top bit cleared so
// the value is positive; TestAdvisoryLockKeyIsDerivedFromTheName recomputes
// it. The value only has to be one no other part of the factory picks, and
// deriving it from a name it will not reuse is how that is arranged.
const AdvisoryLockKey int64 = 0x5888022f314e314d

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than restated,
// so the actor field and its constraints are the same ones every record table
// carries.
//
// prev_hash and hash are unique, which makes the chain a list and not a tree:
// two rows naming the same predecessor is a fork, and the store refuses it
// without waiting for [Verify] to find it. The empty prev_hash of the first
// row is a value like any other under that constraint, so there is at most one
// first row.
//
// The partial unique index is the same rule for closings: one open event
// takes at most one close event, and the store refuses the second without
// waiting for a reader to notice two verdicts over one firing.
var DDL = []string{
	`create sequence if not exists ` + Sequence + ` as bigint`,

	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	seq bigint not null unique,
	shape text not null,
	payload text not null,
	policy_version text not null,
	score_version text not null,
	part text not null,
	closes text not null,
	prev_hash text not null unique,
	hash text not null unique,
	` + record.Constraints + `,
	constraint shape_known check (shape in ('decision', 'page_event', 'wait')),
	constraint part_matches_shape check (
		(shape = 'decision' and part in ('opening', 'closing'))
		or (shape <> 'decision' and part = '')
	),
	constraint versions_match_part check (
		(part = 'opening' and policy_version <> '' and score_version <> '')
		or (part <> 'opening' and policy_version = '' and score_version = '')
	),
	constraint closes_matches_part check (
		(part = 'closing' and closes <> '')
		or (part <> 'closing' and closes = '')
	)
)`,

	`create unique index if not exists decision_log_one_closing on ` + Table + ` (closes) where part = 'closing'`,
}
