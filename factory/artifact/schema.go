package artifact

import "github.com/dulguun0225/borg/factory/record"

// Table is the one table this package owns.
const Table = "artifact"

// IDPrefix is what [record.NewID] is called with for a row of this table.
const IDPrefix = "art"

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than
// restated, so the actor field and its constraints are the same ones every
// record table carries.
//
// The unique constraint on (item_id, kind, version) is what keeps the
// version chain a chain: two submissions that read the same prior version
// would write the same next one, and the store refuses the second rather
// than holding a lock. item_id is an id field and not a foreign key, checked
// for being present and not for pointing at anything — doc.go says what that
// costs.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	item_id text not null,
	kind text not null,
	version int not null,
	supersedes text not null,
	authorship text not null,
	content text not null,
	` + record.Constraints + `,
	constraint item_id_present check (item_id <> ''),
	constraint kind_known check (kind in ('spec', 'implementation')),
	constraint version_starts_at_one check (version >= 1),
	constraint authorship_known check (authorship in ('agent', 'human', 'gate')),
	constraint one_row_per_version unique (item_id, kind, version)
)`,
}
