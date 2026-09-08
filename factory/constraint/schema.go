package constraint

import "github.com/dulguun0225/borg/factory/record"

// Table is the constraint record's table. It is named constraint_record and
// not constraint: constraint is a reserved word in SQL, and every other
// table's DDL in this module is composed into the statement text unquoted, so
// this table takes the record's name with _record appended rather than the
// bare word.
const Table = "constraint_record"

// IDPrefix is what [record.NewID] is called with for a constraint.
const IDPrefix = "cst"

// FormatVersion is written into every constraint record's format_version
// column.
const FormatVersion = "constraint/1"

// Kind is what a constraint's kind decides: where it is checked. The design
// names six kinds; this milestone builds two of them, [KindDocument] and
// [KindNotice], so those are the only values [Kinds] carries and the CHECK in
// [DDL] accepts. Widening both to the other four is M12's.
type Kind string

// KindDocument is a constraint decided by nobody: what enforces it is a
// safeguard putting a human at a gate, and a document-kind constraint may also
// require seam 5 enforced, read at dispatch.
const KindDocument Kind = "document"

// KindNotice is the sixth kind: the text the way in shows a reporter before
// anything is submitted. It is read by no drafting stage and rejects
// nothing, and its reach is always [ReachProject] — [New.validate] refuses
// any other with [ErrNoticeReachMustBeProject].
const KindNotice Kind = "notice"

// Kinds is every kind this milestone accepts. The CHECK in [DDL] lists the
// same two, and TestDDLListsEveryKind fails if the two stop agreeing.
var Kinds = []Kind{KindDocument, KindNotice}

// Reach is what a constraint binds: the factory, one project, one area, or one
// intent.
type Reach string

const (
	// ReachFactory is the widest reach: the constraint binds every item the
	// factory ever decomposes, and names no subject.
	ReachFactory Reach = "factory"
	// ReachProject binds every item inside one project.
	ReachProject Reach = "project"
	// ReachArea binds every item inside one area or any area below it.
	ReachArea Reach = "area"
	// ReachIntent is the narrowest reach: the constraint arrives with a
	// single request and binds every item decomposed from it.
	ReachIntent Reach = "intent"
)

// Reaches is every reach a constraint may have. The CHECK in [DDL] lists the
// same four, and TestDDLListsEveryReach fails if the two stop agreeing.
var Reaches = []Reach{ReachFactory, ReachProject, ReachArea, ReachIntent}

// DDL is this package's schema. [record.Columns] and [record.Constraints] are
// composed rather than restated.
//
// subject_id is empty exactly where reach is factory: the factory reach binds
// everything and names no subject, and every other reach names one — the
// project, the area, or the intent the reach picks out.
//
// binds_from_date and binds_from_zone are both set or both null, the way
// every authored calendar value in the factory carries beside it the zone it
// was authored in; review_by_date and review_by_zone carry the same rule.
// Neither pair carries a CHECK on the date's own shape: the writer parses it
// with time.ParseInLocation before it is ever stored, the way package
// people's spend ceiling validates its own start date.
//
// replaces_id is empty, or the id of the record this one replaced — a
// constraint is never edited, so a correction is a withdrawal and a second
// record naming the one it replaces.
//
// withdrawn_at is set once, by the same write on any reach, and never
// cleared: a constraint whose reach is one intent is withdrawn at Work on
// that intent, and a permanent one at Factory, but both write the same
// column.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	kind text not null,
	reach text not null,
	subject_id text not null,
	statement text not null,
	requires_seam5_enforced boolean not null default false,
	binds_from_date text,
	binds_from_zone text,
	review_by_date text,
	review_by_zone text,
	replaces_id text not null,
	withdrawn_at text,
	` + record.Constraints + `,
	constraint kind_known check (kind in ('document', 'notice')),
	constraint reach_known check (reach in ('factory', 'project', 'area', 'intent')),
	constraint subject_id_matches_reach check (
		(reach = 'factory' and subject_id = '') or (reach <> 'factory' and subject_id <> '')),
	constraint statement_present check (statement <> ''),
	constraint binds_from_whole check (
		(binds_from_date is null and binds_from_zone is null)
		or (binds_from_date is not null and binds_from_zone is not null)),
	constraint review_by_whole check (
		(review_by_date is null and review_by_zone is null)
		or (review_by_date is not null and review_by_zone is not null)),
	constraint withdrawn_at_is_time_layout check (withdrawn_at is null or withdrawn_at ~ '` + record.TimePattern + `')
)`,

	`create index if not exists constraint_record_by_reach_and_subject on ` + Table + ` (reach, subject_id)`,
}
