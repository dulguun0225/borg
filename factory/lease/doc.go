// Package lease holds the store's account of itself: the one row that makes
// exactly one instance of the factory run at a time, and the fencing token
// every write elsewhere in the store is checked against.
//
// # The code
//
// schema.go holds [Table] and [DDL]. lease.go holds [Token], [Acquire],
// [Release], [Renew], and [Fence], and the three errors: [ErrHeld], returned by
// [Acquire] against a lease that is held and has not expired, whichever name
// asks; [ErrFenced], returned by
// [Renew] and by [Fence] against a token that is not the lease's current
// number; and [ErrNoLease], returned by [Fence] where the table holds no row.
//
// This package is not a record package: it composes neither record.Columns
// nor record.Constraints, and it owns no row a link walk ever follows — the
// lease is not a record of the graph, and holds no actor and no format
// version. What it shares with a record package is only [record.TimeLayout],
// for expires_at, and [record.FormatTime] and [record.ParseTime] to write and
// read it.
//
// Who may write what: [Acquire], [Release] and [Renew] are the only writers of
// the lease row — the process that starts, the same process stopping cleanly,
// and that process's own renewal pass. [Fence] never writes it; every other writer in
// the module calls [Fence] inside its own write transaction before the write
// it guards, so a call whose token has lapsed commits nothing.
//
// What defines it: the lease and the fencing token are the deployment model
// stated in ../../end-goal/one-process.md.
package lease
