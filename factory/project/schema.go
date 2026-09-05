package project

import "github.com/dulguun0225/borg/factory/record"

// Table is the one table this package owns.
const Table = "project"

// IDPrefix is what [record.NewID] is called with for a project.
const IDPrefix = "prj"

// FormatVersion is what this package writes into format_version on every
// insert into [Table].
const FormatVersion = "project/1"

// DDL is this package's schema. [record.Columns] and [record.Constraints] are
// composed rather than restated, so the actor field and its constraints are the
// same ones every record table carries.
//
// The name is unique in the store, so two owners writing one project name is a
// refused insert and not two projects. There is no other column: the record
// holds its identity and nothing an owner authors.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	name text not null unique,
	` + record.Constraints + `,
	constraint name_present check (name <> '')
)`,
}
