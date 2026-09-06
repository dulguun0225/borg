// Package postgres opens the pool every writer holds and applies the schema
// every package that owns a table declares.
//
// # The code
//
// postgres.go holds [Open], [URL], which reads [URLEnv] and falls back to
// [DefaultURL], and [Apply]. history.go holds the store's account of itself:
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
// against it; it is dropped and applied again. What says which version the
// store is at is the schema history [Start] reads and writes — one row per
// change, naming the version that shipped it, the change's identity, a
// checksum of its text, and whether it widened the store or removed something
// from it — and what a version's first start refuses is stated there. Applying
// a change is still [Apply]'s flat list: a declared change names the text it
// stands for and the history records what that text was.
//
// Which caller is not built: the install's first-start step, which is what
// calls [Start], writes the install event naming the changes it applied, and
// takes and verifies the snapshot a removal is applied after. The
// command-line interface calls [Apply] and reads no history.
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
// ../../roadmap.md#m0--the-graph-and-the-log. The schema history, the forward
// promise it carries, and what a version's first start refuses are
// ../../end-goal/one-process.md; the shape that promise takes for a service's
// own store is
// ../../end-goal/how-the-factory-works/07-contracts/09-the-store-is-a-contract-too.md.
package postgres
