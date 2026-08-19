package environment

import "github.com/dulguun0225/borg/factory/record"

// Table is the environment table this package owns.
const Table = "environment"

// ThresholdTable is the gate-threshold table this package owns: one row per
// environment and gate row an owner authored a threshold for.
const ThresholdTable = "environment_gate_threshold"

// IDPrefix is what [record.NewID] is called with for an environment.
const IDPrefix = "env"

// ThresholdIDPrefix is what [record.NewID] is called with for a threshold row.
const ThresholdIDPrefix = "egt"

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than restated.
//
// The kind CHECK lists production alone, the kind this milestone writes;
// doc.go says why, and what widening it later costs. The name is unique, so
// production is one record until a project record exists to have one each.
//
// A threshold row exists only where an owner authored one: an absent row is the
// score supplying the value, which is why the column is not null here and the
// distinction is the row's presence rather than a null inside it. The unique
// constraint is what an authoring write conflicts on, so re-authoring a
// threshold is one row and not two.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	kind text not null,
	name text not null unique,
	targets text not null,
	credential text not null,
	` + record.Constraints + `,
	constraint kind_known check (kind in ('production')),
	constraint name_present check (name <> ''),
	constraint targets_present check (targets <> ''),
	constraint credential_present check (credential <> '')
)`,

	`create table if not exists ` + ThresholdTable + ` (
	` + record.Columns + `,
	environment_id text not null,
	gate_row text not null,
	threshold double precision not null,
	` + record.Constraints + `,
	constraint environment_id_present check (environment_id <> ''),
	constraint gate_row_present check (gate_row <> ''),
	constraint threshold_in_range check (threshold >= 0 and threshold <= 1),
	constraint one_row_per_environment_and_gate unique (environment_id, gate_row)
)`,
}
