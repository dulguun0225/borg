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
//
// The four parameter columns are null where an owner authored nothing, which is
// not the same as a value of zero: an unauthored parameter is the score's to
// supply. They are the second writer's columns and the cut never writes them,
// which is the seam parameters.go states.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	name text not null unique,
	repository text not null,
	window_size double precision,
	window_confidence double precision,
	window_cap_seconds double precision,
	k double precision,
	` + record.Constraints + `,
	constraint name_present check (name <> ''),
	constraint repository_present check (repository <> ''),
	constraint window_size_is_a_share check (window_size is null or (window_size > 0 and window_size <= 1)),
	constraint window_confidence_is_a_share check (window_confidence is null or (window_confidence > 0 and window_confidence <= 1)),
	constraint window_cap_positive check (window_cap_seconds is null or window_cap_seconds > 0),
	constraint k_positive check (k is null or k > 0)
)`,
}
