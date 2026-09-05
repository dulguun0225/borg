package area

import "github.com/dulguun0225/borg/factory/record"

// Table is the one table this package owns.
const Table = "area"

// IDPrefix is what [record.NewID] is called with for an area.
const IDPrefix = "ar"

// FormatVersion is what this package writes into format_version on every
// insert into [Table].
const FormatVersion = "area/2"

// DDL is this package's schema. [record.Columns] and [record.Constraints] are
// composed rather than restated, so the actor field and its constraints are
// the same ones every record table carries.
//
// The name is unique, so two owners declaring one name is a refused insert and
// not two areas.
//
// An area lies inside exactly one thing: another area, or the project the chain
// ends at. That is two columns and not one, because the two are different kinds
// of record and a single column holding either would be read by joining against
// both tables and hoping one answers. inside_is_one_of_two is what refuses an
// area inside both and an area inside neither, so no chain runs off the end.
//
// The grade is empty on an area that names none, which is a real state: where no
// area in a chain names one the value in force is negligible, and Factory reports
// how many declared areas name none. irreversible_names_its_operation_and_bound
// is the store's half of the refusal writer.go makes: the grade is not written
// without the hazardous operation, the bound, and the period the bound is
// counted over.
//
// item_size_target is null where an owner authored nothing, which is not the
// same as a target of zero — an unauthored parameter is the score's to supply
// and a zero would be a target every item exceeds. Its unit is the count of its
// intent's requirements an item answers.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	name text not null unique,
	inside_area_id text not null,
	project_id text not null,
	grade text not null,
	hazardous_operation text not null,
	hazard_bound double precision,
	hazard_bound_period_seconds double precision,
	item_size_target double precision,
	` + record.Constraints + `,
	constraint name_present check (name <> ''),
	constraint inside_is_one_of_two check ((inside_area_id <> '') <> (project_id <> '')),
	constraint grade_known check (grade in ('', 'negligible', 'recoverable', 'irreversible')),
	constraint irreversible_names_its_operation_and_bound check (
		grade <> 'irreversible'
		or (hazardous_operation <> '' and hazard_bound is not null and hazard_bound_period_seconds is not null)
	),
	constraint hazard_bound_positive check (hazard_bound is null or hazard_bound > 0),
	constraint hazard_bound_period_positive check (hazard_bound_period_seconds is null or hazard_bound_period_seconds > 0),
	constraint item_size_target_positive check (item_size_target is null or item_size_target > 0)
)`,
}
