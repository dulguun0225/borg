package project

import "github.com/dulguun0225/borg/factory/record"

// Table is the one table this package owns.
const Table = "project"

// IDPrefix is what [record.NewID] is called with for a project.
const IDPrefix = "prj"

// FormatVersion is what this package writes into format_version on every
// insert into [Table].
const FormatVersion = "project/2"

// DDL is this package's schema. [record.Columns] and [record.Constraints] are
// composed rather than restated, so the actor field and its constraints are the
// same ones every record table carries.
//
// The name is unique in the store, so two owners writing one project name is a
// refused insert and not two projects.
//
// ended_at is when an owner ended the project at Factory, once every service in
// it was retired, and is empty while it stands. It is the one column beside the
// identity, and it is a timestamp rather than a deletion: the row stays, so an
// area, a constraint, a safeguard or a scope naming the project never points at
// nothing.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	name text not null unique,
	ended_at text not null default '',
	` + record.Constraints + `,
	constraint name_present check (name <> ''),
	constraint ended_at_is_time_layout check (ended_at = '' or ended_at ~ '` + record.TimePattern + `')
)`,
}
