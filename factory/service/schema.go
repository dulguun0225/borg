package service

import "github.com/dulguun0225/borg/factory/record"

// Table is the one table this package owns.
const Table = "service"

// IDPrefix is what [record.NewID] is called with for a service.
const IDPrefix = "svc"

// DDL is this package's schema. [record.Columns] and [record.Constraints] are
// composed rather than restated, so the actor field and its constraints are
// the same ones every record table carries.
//
// The name is unique in the store, so two cuts creating one service name is a
// refused insert and not two services.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	name text not null unique,
	repository text not null,
	` + record.Constraints + `,
	constraint name_present check (name <> ''),
	constraint repository_present check (repository <> '')
)`,
}
