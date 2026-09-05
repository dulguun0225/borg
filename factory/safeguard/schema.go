package safeguard

import "github.com/dulguun0225/borg/factory/record"

// Table is the one table this package owns.
const Table = "safeguard"

// IDPrefix is what [record.NewID] is called with for a safeguard.
const IDPrefix = "sfg"

// FormatVersion is written into every safeguard record's format_version
// column.
const FormatVersion = "safeguard/1"

// DDL is this package's schema. [record.Columns] and [record.Constraints] are
// composed rather than restated.
//
// The bound is four columns because a parameter's value is a number, a list, or
// one predicate, and never two of the three: bound is null unless the parameter's
// kind is numeric, bound_list is empty unless it is a list, the two predicate
// columns are empty unless it is a predicate, and a safeguard that adds a human
// carries none of them. The CHECK refuses more than one at once, which is as far
// as the store can go — which shape a parameter takes is package gatepolicy's
// table, and restating it here would be one fact in two places able to disagree.
//
// withdrawn is a field and not a second record: a withdrawn safeguard stays
// readable beside the mechanism that stopped reading it, and the design's own
// account of a safeguard names withdrawal as an attribute of the safeguard.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	parameter text not null,
	subject_kind text not null,
	subject_id text not null,
	direction text not null,
	bound double precision,
	bound_list text not null,
	predicate_kind text not null,
	predicate_argument text not null,
	withdrawn boolean not null,
	` + record.Constraints + `,
	constraint parameter_present check (parameter <> ''),
	constraint subject_kind_known check (subject_kind in ('service', 'area', 'gate_row',
		'factory_settings', 'contract_element')),
	constraint subject_id_present check (subject_id <> ''),
	constraint direction_known check (direction in ('ceiling', 'floor', 'adds_a_human')),
	constraint one_bound_at_most check (
		(case when bound is null then 0 else 1 end)
		+ (case when bound_list = '' then 0 else 1 end)
		+ (case when predicate_kind = '' then 0 else 1 end) <= 1),
	constraint predicate_argument_needs_a_kind check (predicate_kind <> '' or predicate_argument = '')
)`,

	`create index if not exists safeguard_by_subject on ` + Table + ` (subject_kind, subject_id)`,
}
