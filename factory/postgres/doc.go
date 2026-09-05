// Package postgres opens the pool every writer holds and applies the schema
// every package that owns a table declares.
//
// # The code
//
// postgres.go holds [Open], [URL], which reads [URLEnv] and falls back to
// [DefaultURL], and [Apply]. There is no ORM and no migration framework:
// [Apply] is the list, naming each package that owns a table and running the
// DDL that package exported, in the order written there. Learning the whole
// schema is reading that function and following it to each package's DDL —
// there is no directory of numbered files, no registry a package adds itself
// to at init, and nothing that discovers a table. Adding a table is an edit
// here and an edit there.
//
// Every statement is written to be applied to a database that already has it,
// which makes [Apply] safe to run at every start. Exactly one process runs at
// a time, and what enforces that is not a convention but package lease, whose
// row this package applies first: a starting process acquires the lease
// before it does anything else, and two processes applying [Apply] at once is
// the case the lease exists to keep from happening, since IF NOT EXISTS does
// not make it safe on its own — PostgreSQL checks for the object and creates
// it as two steps, so both can pass the check and the loser fails on a
// catalogue index. The lease and the fencing token every write after it
// carries are the deployment model, ../../end-goal/one-process.md.
//
// CREATE TABLE IF NOT EXISTS does not alter a table that is already there, so
// a database written under an earlier schema is not brought forward by running
// against it; it is dropped and applied again. Nothing here detects that: the
// first write against a table missing a column fails on the column, which is
// not the same as a store that knows what version it is at.
//
// Who may write what: this package creates the schema and writes no record. It
// imports the packages that own tables; they do not import it, because that
// would be the cycle the compiler refuses. A database test in one of those
// packages reaches the pool from its external test package, which is the edge
// deps.txt records as "test decisionlog -> postgres secretref".
//
// What defines it: the store is where the five seams of "Security comes last"
// are written, ../../end-goal/deferred.md#security-comes-last. PostgreSQL from
// the first record with no migration framework is
// ../../roadmap.md#m0--the-graph-and-the-log, and the forward promise this
// store does not have is due where ../../end-goal/one-process.md states the
// schema history that promise is the shape of.
package postgres
