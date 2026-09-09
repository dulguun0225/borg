package fleetentry

import "github.com/dulguun0225/borg/factory/record"

// Table is the fleet entry record's table.
const Table = "fleet_entry"

// IDPrefix is what [record.NewID] is called with for a fleet entry.
const IDPrefix = "fle"

// FormatVersion is written into every fleet entry's format_version column.
const FormatVersion = "fleet_entry/1"

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than
// restated.
//
// scope_project_id, scope_service_id and scope_area_id are the three columns
// a scope narrows to; every one of the three may be empty, and an entry
// naming none of them is scoped to the whole factory.
//
// effort may be empty: an entry asking the provider for no effort. role is
// checked only for being present — the role vocabulary is package dispatch's,
// which this package cannot import without a cycle, so a role dispatch does
// not recognise is a hold at dispatch and not a refusal here.
//
// material_classes is a newline-joined list of the seven constants this
// package declares, [ClassIntentStatement] through [ClassFailureRecords], the
// way agentrun's sources and skill version ids are joined; an entry naming
// none is handed none. The writer refuses a class the list does not name and
// a class named twice — nothing here checks either, the store taking whatever
// text the writer sends.
//
// operations is joined the same way and is the owner's narrowing of the list
// the role carries: empty is the role's whole list, and an operation the role
// does not carry is refused where the role's list is known, which is package
// dispatch and not here. It is not a tenth field of the design's nine: the
// entry reaches the list by naming the role, and this column holds the
// narrowing an owner may write over it and never a list of its own.
//
// reads_at_once and dispatches_between_evaluation_runs are bigint and each
// carries a CHECK that it is positive: a bound of zero or less is not a value
// either field's design gives a meaning to.
//
// withdrawn_at is nullable rather than a flag: the row is kept, and it is the
// time the entry was withdrawn that marks it out of force, the way a
// safeguard's withdrawal keeps its own approved_at.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	model_version text not null,
	effort text not null,
	role text not null,
	scope_project_id text not null default '',
	scope_service_id text not null default '',
	scope_area_id text not null default '',
	credential_name text not null,
	processing_location text not null,
	material_classes text not null default '',
	operations text not null default '',
	reads_at_once bigint not null,
	dispatches_between_evaluation_runs bigint not null,
	withdrawn_at text,
	` + record.Constraints + `,
	constraint model_version_present check (model_version <> ''),
	constraint role_present check (role <> ''),
	constraint credential_name_present check (credential_name <> ''),
	constraint processing_location_present check (processing_location <> ''),
	constraint reads_at_once_positive check (reads_at_once > 0),
	constraint dispatches_between_evaluation_runs_positive check (dispatches_between_evaluation_runs > 0),
	constraint withdrawn_at_is_time_layout check (withdrawn_at is null or withdrawn_at ~ '` + record.TimePattern + `')
)`,

	`create index if not exists fleet_entry_by_role on ` + Table + ` (role) where withdrawn_at is null`,
}
