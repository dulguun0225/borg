package factorysettings

import "github.com/dulguun0225/borg/factory/record"

// Table is the factory-wide settings table this package owns.
const Table = "factory_settings"

// LimitTable is the attempt-limit table this package owns: one row per stage an
// owner authored a limit for.
const LimitTable = "factory_settings_attempt_limit"

// IDPrefix is what [record.NewID] is called with for the factory-wide settings record.
const IDPrefix = "fs"

// LimitIDPrefix is what [record.NewID] is called with for an attempt-limit row.
const LimitIDPrefix = "fsl"

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than restated.
//
// only_row is what makes the record a singleton: it is always true and it is
// unique, so a second insert is refused by the store and not by whichever
// caller looked first.
//
// allowed_predicate_kinds is text and empty where an owner extended nothing, one kind
// of assertion per line. role_prompt_or_skill_threshold is null where an owner
// authored none, the same distinction every authored parameter carries: an
// unauthored parameter is the score's to supply, and a threshold of zero would
// be an owner asking for a human at every version.
//
// A limit row exists only where an owner authored one, and the unique
// constraint is what re-authoring conflicts on.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	only_row boolean not null default true,
	allowed_predicate_kinds text not null,
	role_prompt_or_skill_threshold double precision,
	` + record.Constraints + `,
	constraint only_row_is_true check (only_row),
	constraint one_factory_settings unique (only_row),
	constraint role_prompt_or_skill_threshold_in_range check (
		role_prompt_or_skill_threshold is null
		or (role_prompt_or_skill_threshold >= 0 and role_prompt_or_skill_threshold <= 1))
)`,

	`create table if not exists ` + LimitTable + ` (
	` + record.Columns + `,
	factory_settings_id text not null,
	stage text not null,
	attempt_limit int not null,
	` + record.Constraints + `,
	constraint factory_settings_id_present check (factory_settings_id <> ''),
	constraint stage_present check (stage <> ''),
	constraint attempt_limit_positive check (attempt_limit > 0),
	constraint one_row_per_stage unique (factory_settings_id, stage)
)`,
}
