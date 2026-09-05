package inputmanifest

import "github.com/dulguun0225/borg/factory/record"

// Table is the one table this package owns.
const Table = "input_manifest"

// IDPrefix is what [record.NewID] is called with for an input manifest.
const IDPrefix = "im"

// FormatVersion is what this package writes into format_version on every
// insert into [Table].
const FormatVersion = "input_manifest/1"

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than
// restated, so the actor field and its constraints are the same ones every
// record table carries.
//
// materials and excluded are JSON arrays of objects rather than a column
// each, because an entry is a small variable-length group of fields and a
// column per field of an array element does not exist. read_at_once_bound is
// nullable because the fleet entry it is read from is not built yet, and
// selection_rule_version is an empty string for the same reason on the
// selection rule.
//
// item_id, stage, and intent_id repeat the shape package agentrun's DDL
// documents: served_names_something is the design's "one of the five and
// never none", narrowed to the two a dispatch exists for today, and a stage
// names nothing without an item.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	item_id text not null,
	stage text not null,
	intent_id text not null,
	materials text not null,
	read_at_once_bound bigint,
	selection_rule_version text not null,
	excluded text not null,
	` + record.Constraints + `,
	constraint served_names_something check (item_id <> '' or intent_id <> ''),
	constraint stage_only_with_an_item check (stage = '' or item_id <> ''),
	constraint read_at_once_bound_nonnegative check (read_at_once_bound is null or read_at_once_bound >= 0)
)`,
}
