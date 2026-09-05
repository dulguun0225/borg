package decisionlog

import "github.com/dulguun0225/borg/factory/record"

// Table is the one table this package owns.
const Table = "decision_log"

// Sequence is where seq comes from. It is declared rather than left to a
// bigserial default because the writer takes the next value itself: seq is
// hashed, so the row's hash cannot be computed until the value is known.
const Sequence = "decision_log_seq"

// IDPrefix is what [record.NewID] is called with for a row of this table,
// whichever of the ten shapes it holds.
const IDPrefix = "dl"

// AdvisoryLockKey is the PostgreSQL advisory lock every append takes for the
// whole of its transaction. It is the first eight bytes of
// SHA-256("borg/factory/decisionlog"), big-endian, with the top bit cleared so
// the value is positive; TestAdvisoryLockKeyIsDerivedFromTheName recomputes
// it. The value only has to be one no other part of the factory picks, and
// deriving it from a name it will not reuse is how that is arranged.
const AdvisoryLockKey int64 = 0x5888022f314e314d

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than
// restated, so the actor field, the format version field, and their
// constraints are the same ones every record table carries.
//
// prev_hash and hash are unique, which makes the chain a list and not a tree:
// two rows naming the same predecessor is a fork, and the store refuses it
// without waiting for [Reader.Verify] to find it. The empty prev_hash of the
// first row is a value like any other under that constraint, so there is at
// most one first row.
//
// format_version_matches_shape is the CHECK named in doc.go: [Formats] and
// this list have to agree, and TestFormatVersionsMatchDDL is what keeps them
// that way. shape_known lists the same ten shapes as [Shapes];
// TestDDLListsEveryShape is what keeps those two agreeing.
//
// part_matches_shape allows a decision its four parts, a wait its two, and
// requires the empty part everywhere else. versions_match_part allows a
// policy version and a score version on a decision's opening and on a
// truncation, and requires both empty everywhere else. closes_matches_part
// requires a closes value on a decision's closing, abandonment, and
// acknowledgement, and on a wait's closing, and requires it empty everywhere
// else. verdict_matches_part allows a verdict, one of the four, only on a
// decision's closing. reason_scope allows a reason only on a decision's
// closing or abandonment; reason_required is where doc.go's reuse of that
// column is enforced: non-empty on an abandonment always, and on a closing
// wherever the verdict is reject or hold. opened_in_work_at_scope and
// self_approval_scope are the same shape of rule for those two columns, both
// of them a decision closing's alone. acknowledgement_actor_human is
// [Writer.AppendDecisionAcknowledgement]'s rule that only a human
// acknowledges, checked again here for a row reaching the store around it.
//
// The three partial unique indexes are what a second ending or a second
// acknowledgement is refused by, whether or not a caller went through the
// methods that check for them first. decision_log_one_closing is a decision's
// own closing, kept distinct from the broader index because doc.go and the
// design both single out the decision's close event by name.
// decision_log_one_ending is broader: at most one closing or abandonment
// names any one id, which is what makes an ended decision's opening refuse a
// second ending of either kind, and what makes a wait's opening — which only
// ever takes a closing — refuse a second one too.
// decision_log_one_acknowledgement_per_human is per human: one opening admits
// any number of acknowledgements, at most one from each.
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
	verdict text not null,
	reason text not null,
	opened_in_work_at text not null,
	self_approval boolean not null default false,
	prev_hash text not null unique,
	hash text not null unique,
	` + record.Constraints + `,
	constraint shape_known check (shape in (
		'decision', 'page_event', 'wait', 'rework_request', 'queue_rejection',
		'truncation', 'policy_version', 'score_version', 'install_event', 'read_event'
	)),
	constraint format_version_matches_shape check (
		(format_version = 'decision/1' and shape = 'decision')
		or (format_version = 'page_event/1' and shape = 'page_event')
		or (format_version = 'wait/1' and shape = 'wait')
		or (format_version = 'rework_request/1' and shape = 'rework_request')
		or (format_version = 'queue_rejection/1' and shape = 'queue_rejection')
		or (format_version = 'truncation/1' and shape = 'truncation')
		or (format_version = 'policy_version/1' and shape = 'policy_version')
		or (format_version = 'score_version/1' and shape = 'score_version')
		or (format_version = 'install_event/1' and shape = 'install_event')
		or (format_version = 'read_event/1' and shape = 'read_event')
	),
	constraint part_matches_shape check (
		(shape = 'decision' and part in ('opening', 'closing', 'abandonment', 'acknowledgement'))
		or (shape = 'wait' and part in ('opening', 'closing'))
		or (shape not in ('decision', 'wait') and part = '')
	),
	constraint versions_match_part check (
		(shape = 'decision' and part = 'opening' and policy_version <> '' and score_version <> '')
		or (shape = 'truncation' and policy_version <> '' and score_version <> '')
		or (
			not (shape = 'decision' and part = 'opening') and shape <> 'truncation'
			and policy_version = '' and score_version = ''
		)
	),
	constraint closes_matches_part check (
		(shape = 'decision' and part in ('closing', 'abandonment', 'acknowledgement') and closes <> '')
		or (shape = 'wait' and part = 'closing' and closes <> '')
		or (
			not (
				(shape = 'decision' and part in ('closing', 'abandonment', 'acknowledgement'))
				or (shape = 'wait' and part = 'closing')
			)
			and closes = ''
		)
	),
	constraint verdict_matches_part check (
		(shape = 'decision' and part = 'closing' and verdict in ('approve', 'reject', 'hold', 'refer'))
		or (not (shape = 'decision' and part = 'closing') and verdict = '')
	),
	constraint reason_scope check (
		(shape = 'decision' and part in ('closing', 'abandonment')) or reason = ''
	),
	constraint reason_required check (
		not (shape = 'decision' and part = 'closing' and verdict in ('reject', 'hold') and reason = '')
		and not (shape = 'decision' and part = 'abandonment' and reason = '')
	),
	constraint opened_in_work_at_scope check (
		(shape = 'decision' and part = 'closing') or opened_in_work_at = ''
	),
	constraint opened_in_work_at_is_time_or_empty check (
		opened_in_work_at = '' or opened_in_work_at ~ '` + record.TimePattern + `'
	),
	constraint self_approval_scope check (
		self_approval = false or (shape = 'decision' and part = 'closing')
	),
	constraint acknowledgement_actor_human check (
		part <> 'acknowledgement' or actor_kind = 'human'
	)
)`,

	`create unique index if not exists decision_log_one_closing on ` + Table +
		` (closes) where shape = 'decision' and part = 'closing'`,
	`create unique index if not exists decision_log_one_ending on ` + Table +
		` (closes) where part in ('closing', 'abandonment')`,
	`create unique index if not exists decision_log_one_acknowledgement_per_human on ` + Table +
		` (closes, actor_key) where part = 'acknowledgement'`,
}
