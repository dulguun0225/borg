package deploy

import "github.com/dulguun0225/borg/factory/record"

// Table is the one table this package owns.
const Table = "deploy"

// IDPrefix is what [record.NewID] is called with for a deploy.
const IDPrefix = "dep"

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than restated,
// so the actor field and its constraints are the same ones every record table
// carries.
//
// service_id, environment_id, release_id, and build_id are id fields and not
// foreign keys: each is another package's record, and a cross-package link is a
// field the link walk reads. The environment is named by the record's id and not
// by its name, the environment being a record from this milestone on. The store
// checks each for being present and not for pointing at anything; record's
// doc.go states that rule and its cost once.
//
// build_id is on every deploy — the build is what runs, and a target reports what
// it runs rather than what that was called. release_id is empty exactly on a
// deploy into a candidate's own environment, that deploy happening one gate before
// the number exists.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	service_id text not null,
	environment_id text not null,
	release_id text not null,
	build_id text not null,
	strategy text not null,
	status text not null,
	` + record.Constraints + `,
	constraint service_id_present check (service_id <> ''),
	constraint environment_id_present check (environment_id <> ''),
	constraint build_id_present check (build_id <> ''),
	constraint strategy_known check (strategy in ('straight')),
	constraint status_known check (status in ('started', 'complete', 'rolled_back'))
)`,
}
