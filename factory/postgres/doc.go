// Package postgres opens the pool every writer holds and applies the schema
// every package that owns a table declares.
//
// # The code
//
// postgres.go holds [Open], [URL], which reads [URLEnv] and falls back to
// [DefaultURL], [ApplyLease] and [Apply]. history.go holds the store's account of itself:
// [HistoryTable] and [HistoryDDL], [Version], [Change] with [Effect] and
// [Effects], [Changes] — every change to this store the version this source
// ships as declares — [History], the read of what the store holds, and
// [Start], which reads the history, refuses the four disagreements the forward
// promise names, applies the schema and records what this version declares. There is no ORM and no migration framework:
// [Apply] is the list, naming each package that owns a table and running the
// DDL that package exported, in the order written there. Learning the whole
// schema is reading that function and following it to each package's DDL —
// there is no directory of numbered files, no registry a package adds itself
// to at init, and nothing that discovers a table. Adding a table is an edit
// here and an edit there.
//
// Every statement is written to be applied to a database that already has it,
// which makes [Apply] safe to run at every start. Exactly one process runs at
// a time, and what enforces that is not a convention but package lease. The
// order is [ApplyLease], then the lease, then [Start]: the lease's own table is
// the one thing created before the lease is acquired, because a lease cannot be
// taken in a store whose lease table does not exist, and everything else in the
// store is touched under a held lease. Two processes applying [Apply] at once is
// the case the lease exists to keep from happening, since IF NOT EXISTS does
// not make it safe on its own — PostgreSQL checks for the object and creates
// it as two steps, so both can pass the check and the loser fails on a
// catalogue index. What the order costs is that the lease table is created by a
// process that may then fail to acquire, which leaves an empty table and
// nothing else. The lease and the fencing token every write after it
// carries are the deployment model, ../../end-goal/one-process.md.
//
// CREATE TABLE IF NOT EXISTS does not alter a table that is already there, so
// a database written under an earlier schema is not brought forward by running
// against it; it is dropped and applied again. What says which version the
// store is at is the schema history [Start] reads and writes — one row per
// change, naming the version that shipped it, the change's identity, a
// checksum of its text, and whether it widened the store or removed something
// from it — and what a version's first start refuses is stated there. Applying
// a change is still [Apply]'s flat list: a declared change names the text it
// stands for and the history records what that text was.
//
// The command-line interface calls [Start] at every start, once it holds the
// lease, so the history is read and the four disagreements are refused there.
// What is not built is the rest of the install's first-start step: the install
// event naming the changes [Start] returns, and the snapshot taken and verified
// before a removal is applied.
//
// Who may write what: this package creates the schema and writes no record. It
// imports the packages that own tables; they do not import it, because that
// would be the cycle the compiler refuses. A database test in one of those
// packages reaches the pool from its external test package, which is the edge
// deps.txt records as a test line naming postgres — "test window -> postgres
// lease" and the rest. Package decisionlog's own tests are the exception: they
// apply lease.DDL and decisionlog.DDL directly, so its test line names
// secretref alone.
//
// What defines it: the store is where the five seams of "Security comes last"
// are written, ../../end-goal/deferred.md#security-comes-last. PostgreSQL from
// the first record with no migration framework is
// ../../roadmap.md#m0--the-graph-and-the-log. The schema history, the forward
// promise it carries, and what a version's first start refuses are
// ../../end-goal/one-process.md; the shape that promise takes for a service's
// own store is
// ../../end-goal/how-the-factory-works/07-contracts/09-the-store-is-a-contract-too.md.
package postgres
