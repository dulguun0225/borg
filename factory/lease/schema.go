package lease

// Table is the one table this package owns.
const Table = "lease"

// DDL is this package's schema, in the order the statements are applied. It
// composes neither record.Columns nor record.Constraints: the lease is the
// store's account of itself and not a record of the graph, so it carries
// none of the actor or format-version fields a record table does, and
// ../../end-goal/records.md lists no row for it.
//
// The table holds exactly one row, id = 1, enforced by the primary key
// together with the CHECK: instance is who holds the lease, expires_at is
// when the hold lapses unless renewed, in record.TimeLayout, and number is
// the fencing token — it rises by one at every acquisition and never
// otherwise, and it is what a write elsewhere in the store is checked
// against through [Fence].
var DDL = []string{
	`create table if not exists ` + Table + ` (
	id int primary key check (id = 1),
	instance text not null,
	expires_at text not null,
	number bigint not null
)`,
}
