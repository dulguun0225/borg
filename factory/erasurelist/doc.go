// Package erasurelist is the erasure list: an append-only file on the host,
// outside the recovery unit, naming what a redaction removed and what an
// erased mapping removed — never the words themselves.
//
// # The code
//
// erasurelist.go holds [FormatVersion], [ErrFormatVersion], [Row], the four
// kinds a row may name with [ErrKind] for any other, [Append] and
// [ReadKind]. retire.go holds [Retire], the store's own pass past
// backup retention. erasurelist_test.go is the tests, every one of them over
// a temporary directory and no database.
//
// # The row
//
// One line per row, JSON-encoded with the payload format version first:
// [Row.FormatVersion], the treatment seam 2 gives a log row, because the
// list is never migrated and a later release reading an early row is told
// its shape from that field rather than guessing it from which fields the
// row holds — a row naming any other version is refused with
// [ErrFormatVersion] rather than read as though it matched. Then the key a
// step is taken under: [Append] writes nothing where the file already
// carries a row with that key, so a step taken again does not become a
// second row. Then the instant the row was appended, in
// [record.TimeLayout], the kind of record the erasure or the deletion
// reached, one of [KindReport], [KindStatement], [KindArtifactVersion] and
// [KindMapping], the four stores that replay — [ReadKind] is a read by kind,
// which is how a store replays the rows naming its own records against
// whatever a restore brought back before it serves again — and what was
// removed, described by the caller
// without ever carrying the erased text: a span, a field, or a mapping, not
// the words.
//
// [Retire] is the store's own pass past backup retention: a row is removed
// once the erasure it records is older than the duration the caller
// supplies, and kept otherwise. That duration, backup_retention_seconds, has
// no reader here — it is read by whatever settings record carries it and
// handed to [Retire] as a plain [time.Duration] — and where an owner
// authored none the caller passes zero, which retires nothing: rows are kept
// for the life of the install.
//
// # A departure from the record shape
//
// Every other record package owns a table built from [record.Columns] and
// [record.Constraints], with an actor on every row. This package owns no
// table and no row here carries an actor. The erasure list is a file and not
// a row of the store: it exists to say what a restored store must no longer
// hold, so it cannot itself be a row a backup restores (C0452). And the two
// actions that write here — a redaction and a mapping deletion — already
// carry their own actor on the record each one writes; repeating the field
// on this row would be a second, disconnected copy of it rather than the one
// the design keeps. [driftdetector]'s doc.go states the same kind of
// departure for the same reason: what is stored here is not a record of the
// graph.
//
// This package reads no environment variable and opens no path of its own:
// every function here takes the file's path from its caller, which is the
// composition that decides where the erasure list lives on the host.
//
// Who may write what: [Append] and [Retire] are this package's own, and the
// only way any row here is written or removed. Nothing here reaches a
// database. reportstore is the design's one writer of the file through this
// package — Factory calls it at a redaction and people.DeleteMapping takes
// an appender the composition supplies — neither of which this package
// knows about or imports.
//
// What defines it:
// ../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/02-reports.md
// (C0450, C0452, C0455, C0456, C0457, C0458, C0459) and
// ../../end-goal/records.md (C2794).
package erasurelist
