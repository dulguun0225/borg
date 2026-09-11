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
// [formatVersionMatchesShape] have to agree, and TestFormatVersionsMatchDDL is
// what keeps them that way. shape_known lists the same ten shapes as [Shapes];
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
// closing or abandonment, or a rework request; reason_required is where
// doc.go's reuse of that column is enforced: non-empty on an abandonment
// always, on a rework request always, and on a closing wherever the verdict
// is reject or hold. opened_in_work_at_scope and self_approval_scope are the
// same shape of rule for those two columns, both of them a decision closing's
// alone; opened_in_work_at_caller_is_work is
// [Writer.AppendDecisionClose]'s rule that a non-empty opened_in_work_at
// names the Work screen as the caller, checked again here for a row reaching
// the store around it. acknowledgement_actor_human, abandonment_actor_component
// and wait_open_actor_component are the same shape of rule for the other
// three parts a kind of actor is fixed for: only a human acknowledges, only a
// component abandons, and only a component opens a wait.
//
// returns_to_scope allows a returns_to only on a decision's closing where the
// verdict is reject, or on a rework request; returns_to_required is a rework
// request's alone, a reject being allowed to leave the field empty where the
// row sends nothing back. reading_scope and reading_required, and
// moved_release_scope, are the merge queue rejection's own columns, admitted
// only on that shape; a rejection always names a reading and a moved release
// only where one exists. caller_present_together, caller_kind_known,
// caller_key_basis_known and caller_dispatch_scope_matches_kind are
// [principal.Principal.Validate]'s own rules over the columns a call's
// principal is stored in beside the actor, checked again here: caller_kind,
// caller_key and caller_key_basis are empty together or none of them are, an
// agent caller names a dispatch and a scope and no other kind of caller does,
// and a row naming no caller at all names neither.
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
	returns_to text not null default '',
	reading text not null default '',
	moved_release text not null default '',
	caller_kind text not null default '',
	caller_key text not null default '',
	caller_key_basis text not null default '',
	caller_dispatch_id text not null default '',
	caller_scope text not null default '',
	prev_hash text not null unique,
	hash text not null unique,
	` + record.Constraints + `,
	constraint shape_known check (shape in (
		'decision', 'page_event', 'wait', 'rework_request', 'queue_rejection',
		'truncation', 'policy_version', 'score_version', 'install_event', 'read_event'
	)),
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
		(shape = 'decision' and part in ('closing', 'abandonment')) or shape = 'rework_request' or reason = ''
	),
	constraint reason_required check (
		not (shape = 'decision' and part = 'closing' and verdict in ('reject', 'hold') and reason = '')
		and not (shape = 'decision' and part = 'abandonment' and reason = '')
		and not (shape = 'rework_request' and reason = '')
	),
	constraint opened_in_work_at_scope check (
		(shape = 'decision' and part = 'closing') or opened_in_work_at = ''
	),
	constraint opened_in_work_at_is_time_or_empty check (
		opened_in_work_at = '' or opened_in_work_at ~ '` + record.TimePattern + `'
	),
	constraint opened_in_work_at_caller_is_work check (
		opened_in_work_at = '' or (caller_kind = 'component' and caller_key = 'work')
	),
	constraint self_approval_scope check (
		self_approval = false or (shape = 'decision' and part = 'closing')
	),
	constraint acknowledgement_actor_human check (
		part <> 'acknowledgement' or actor_kind = 'human'
	),
	constraint abandonment_actor_component check (
		part <> 'abandonment' or actor_kind = 'component'
	),
	constraint wait_open_actor_component check (
		not (shape = 'wait' and part = 'opening') or actor_kind = 'component'
	),
	constraint returns_to_scope check (
		(shape = 'decision' and part = 'closing' and verdict = 'reject')
		or shape = 'rework_request' or returns_to = ''
	),
	constraint returns_to_required check (
		not (shape = 'rework_request' and returns_to = '')
	),
	constraint reading_scope check (
		shape = 'queue_rejection' or reading = ''
	),
	constraint reading_required check (
		not (shape = 'queue_rejection' and reading = '')
	),
	constraint moved_release_scope check (
		shape = 'queue_rejection' or moved_release = ''
	),
	constraint caller_present_together check (
		(caller_kind = '') = (caller_key = '') and (caller_kind = '') = (caller_key_basis = '')
	),
	constraint caller_kind_known check (
		caller_kind = '' or caller_kind in ('human', 'component', 'agent')
	),
	constraint caller_key_basis_known check (
		caller_key_basis = '' or caller_key_basis in ('claimed', 'verified')
	),
	constraint caller_dispatch_scope_matches_kind check (
		(caller_kind = 'agent' and caller_dispatch_id <> '' and caller_scope <> '')
		or (caller_kind <> 'agent' and caller_dispatch_id = '' and caller_scope = '')
	)
)`,

	// A store this package already applied before these columns existed has
	// the table without them; these are the same statements a fresh create
	// already carries, added rather than assumed, so both paths agree.
	`alter table ` + Table + ` add column if not exists returns_to text not null default ''`,
	`alter table ` + Table + ` add column if not exists reading text not null default ''`,
	`alter table ` + Table + ` add column if not exists moved_release text not null default ''`,
	`alter table ` + Table + ` add column if not exists caller_kind text not null default ''`,
	`alter table ` + Table + ` add column if not exists caller_key text not null default ''`,
	`alter table ` + Table + ` add column if not exists caller_key_basis text not null default ''`,
	`alter table ` + Table + ` add column if not exists caller_dispatch_id text not null default ''`,
	`alter table ` + Table + ` add column if not exists caller_scope text not null default ''`,

	`alter table ` + Table + ` drop constraint if exists format_version_matches_shape`,
	`alter table ` + Table + ` add constraint format_version_matches_shape check (` +
		formatVersionMatchesShape + `) not valid`,

	`create unique index if not exists decision_log_one_closing on ` + Table +
		` (closes) where shape = 'decision' and part = 'closing'`,
	`create unique index if not exists decision_log_one_ending on ` + Table +
		` (closes) where part in ('closing', 'abandonment')`,
	`create unique index if not exists decision_log_one_acknowledgement_per_human on ` + Table +
		` (closes, actor_key) where part = 'acknowledgement'`,
}

// formatVersionMatchesShape is the CHECK's own expression, held apart because
// [DDL] writes the constraint with an ALTER rather than inside the CREATE
// TABLE: a shape that gains a format version widens this list, and an install
// created before that version holds the narrower one — `create table if not
// exists` alters nothing that is already there, so the constraint is dropped
// and written again at every start, which is how an install written by one
// version of the factory is read by the next.
//
// It is added NOT VALID, which validates every row appended after it and
// scans none already there. Every widening of this list admits format versions
// the narrower one refused, so the rows in the log were written under a
// stricter rule than the one being added and a scan could find nothing.
const formatVersionMatchesShape = `
		(format_version = 'decision/1' and shape = 'decision')
		or (format_version = 'page_event/2' and shape = 'page_event')
		or (format_version = 'page_event/1' and shape = 'page_event')
		or (format_version = 'wait/1' and shape = 'wait')
		or (format_version = 'rework_request/1' and shape = 'rework_request')
		or (format_version = 'queue_rejection/1' and shape = 'queue_rejection')
		or (format_version = 'truncation/1' and shape = 'truncation')
		or (format_version = 'policy_version/1' and shape = 'policy_version')
		or (format_version = 'score_version/1' and shape = 'score_version')
		or (format_version = 'install_event/1' and shape = 'install_event')
		or (format_version = 'read_event/1' and shape = 'read_event')
	`
