// Package postgres opens the pool every writer holds and applies the schema
// every package that owns a table declares.
//
// There is no ORM and no migration framework. [Apply] is the list: it names
// each package that owns a table and runs the DDL that package exported, in
// the order it is written there. What a reader has to do to learn the whole
// schema is read that function and follow it to each package's DDL — there is
// no directory of numbered files, no registry a package adds itself to at
// init, and nothing that discovers a table. Adding a table is an edit here and
// an edit there.
//
// Every statement is written to be applied to a database that already has it,
// which makes [Apply] safe to run at every start of one process at a time.
// Two processes applying at once is not safe, and IF NOT EXISTS does not make
// it safe: PostgreSQL checks for the object and creates it as two steps, so
// both can pass the check and the loser fails on a catalogue index —
// pg_class_relname_nsp_index for the sequence, pg_type_typname_nsp_index for
// the table, which creates a composite type of its own name. Nothing but the
// tests calls Apply today, and they run one at a time. What it costs later is
// that starting two replicas together needs something this package does not
// have — a lock around Apply, or applying the schema somewhere other than at
// a process start.
//
// That is the whole of the forward promise today: the factory is at the
// milestone where the schema is created and never yet changed, and what a
// change to an existing table costs is a question the store has not had to
// answer.
//
// Who may write what: this package creates the schema and writes no record.
// It imports the packages that own tables; they do not import it, because that
// would be the cycle the compiler refuses. A database test in one of those
// packages reaches the pool from its external test package, which is the edge
// deps.txt records as "test decisionlog -> postgres secretref".
//
// What defines it: the store is where the four seams of "Security comes last"
// are written, ../../end-goal/deferred.md#security-comes-last, and the choice
// of PostgreSQL from the first record with no migration framework is
// ../../roadmap.md#m0--the-graph-and-the-log.
package postgres
