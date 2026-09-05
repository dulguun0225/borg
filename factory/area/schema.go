package area

import "github.com/dulguun0225/borg/factory/record"

// Table is the one table this package owns.
const Table = "area"

// IDPrefix is what [record.NewID] is called with for an area.
const IDPrefix = "ar"

// FormatVersion is what this package writes into format_version on every
// insert into [Table].
const FormatVersion = "area/1"

// DDL is this package's schema. [record.Columns] and [record.Constraints] are
// composed rather than restated, so the actor field and its constraints are
// the same ones every record table carries.
//
// The name is unique, so two owners declaring one name is a refused insert and
// not two areas. inside is the area this one lies inside and is empty at the
// outermost, so it is checked for nothing: the present rule record's doc.go
// states applies to a link that must name something, and this one may name
// nothing.
//
// item_size_target is null where an owner authored nothing, which is not the
// same as a target of zero — an unauthored parameter is the score's to supply
// and a zero would be a target every item exceeds. Every authored parameter in
// the factory carries that distinction the same way.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	name text not null unique,
	inside text not null,
	item_size_target double precision,
	` + record.Constraints + `,
	constraint name_present check (name <> ''),
	constraint item_size_target_positive check (item_size_target is null or item_size_target > 0)
)`,
}
