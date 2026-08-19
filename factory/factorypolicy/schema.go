package factorypolicy

import "github.com/dulguun0225/borg/factory/record"

// Table is the factory policy table this package owns.
const Table = "factory_policy"

// BoundTable is the attempt-bound table this package owns: one row per stage an
// owner authored a bound for.
const BoundTable = "factory_policy_attempt_bound"

// IDPrefix is what [record.NewID] is called with for the factory policy record.
const IDPrefix = "fp"

// BoundIDPrefix is what [record.NewID] is called with for an attempt-bound row.
const BoundIDPrefix = "fpb"

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than restated.
//
// only_row is what makes the record a singleton: it is always true and it is
// unique, so a second insert is refused by the store and not by whichever
// caller looked first.
//
// predicate_catalog is text and empty where an owner extended nothing, one kind
// of assertion per line. brief_or_skill_threshold is null where an owner
// authored none, the same distinction every authored parameter carries: an
// unauthored parameter is the score's to supply, and a threshold of zero would
// be an owner asking for a human at every version.
//
// A bound row exists only where an owner authored one, and the unique
// constraint is what re-authoring conflicts on.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	only_row boolean not null default true,
	predicate_catalog text not null,
	brief_or_skill_threshold double precision,
	` + record.Constraints + `,
	constraint only_row_is_true check (only_row),
	constraint one_factory_policy unique (only_row),
	constraint brief_or_skill_threshold_in_range check (
		brief_or_skill_threshold is null
		or (brief_or_skill_threshold >= 0 and brief_or_skill_threshold <= 1))
)`,

	`create table if not exists ` + BoundTable + ` (
	` + record.Columns + `,
	factory_policy_id text not null,
	stage text not null,
	bound int not null,
	` + record.Constraints + `,
	constraint factory_policy_id_present check (factory_policy_id <> ''),
	constraint stage_present check (stage <> ''),
	constraint bound_positive check (bound > 0),
	constraint one_row_per_stage unique (factory_policy_id, stage)
)`,
}
