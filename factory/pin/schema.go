package pin

import "github.com/dulguun0225/borg/factory/record"

// Table is the one table this package owns.
const Table = "pin"

// IDPrefix is what [record.NewID] is called with for a pin.
const IDPrefix = "pin"

// DDL is this package's schema. [record.Columns] and [record.Constraints] are
// composed rather than restated.
//
// The bound is two columns because a parameter's value is a number or a list
// and never both: bound is null unless the parameter's kind is numeric,
// bound_list is empty unless it is a list, and a pin that adds a human carries
// neither. The CHECK refuses both at once, which is as far as the store can go
// — which of the three a parameter takes is package gatepolicy's table, and
// restating it here would be one fact in two places able to disagree.
//
// withdrawn is a field and not a second record: a withdrawn pin stays readable
// beside the mechanism that stopped reading it, and the design's own account of
// a pin names withdrawal as an attribute of the pin.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	parameter text not null,
	subject_kind text not null,
	subject_id text not null,
	direction text not null,
	bound double precision,
	bound_list text not null,
	withdrawn boolean not null,
	` + record.Constraints + `,
	constraint parameter_present check (parameter <> ''),
	constraint subject_kind_known check (subject_kind in ('service', 'area', 'gate_row', 'factory_policy')),
	constraint subject_id_present check (subject_id <> ''),
	constraint direction_known check (direction in ('ceiling', 'floor', 'adds_a_human')),
	constraint one_bound_at_most check (bound is null or bound_list = '')
)`,

	`create index if not exists pin_by_subject on ` + Table + ` (subject_kind, subject_id)`,
}
