package build

import "github.com/dulguun0225/borg/factory/record"

// Table is the one table this package owns.
const Table = "build"

// IDPrefix is what [record.NewID] is called with for a build.
const IDPrefix = "bl"

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than restated,
// so the actor field and its constraints are the same ones every record table
// carries.
//
// item_id is an id field and not a foreign key: the item is another package's
// record, and a cross-package link is a field the link walk reads. The store
// checks it for being present and not for pointing at anything; record's
// doc.go states that rule and its cost once.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	item_id text not null,
	commit_hash text not null,
	` + record.Constraints + `,
	constraint item_id_present check (item_id <> ''),
	constraint commit_hash_present check (commit_hash <> ''),
	constraint one_build_per_commit unique (item_id, commit_hash)
)`,
}
