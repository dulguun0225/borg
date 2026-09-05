// Package record holds the conventions every record in the graph follows: the
// actor that wrote it, its identifier, the time it was written, and the SQL
// text a table composes for its columns and their constraints.
//
// # The code
//
// actor.go holds [Actor] — a [Kind], one of [Kinds], and a name — and
// [Actor.Validate], which refuses an unknown kind and an empty name.
// columns.go holds [Columns] and [Constraints], the SQL a table composes into
// its own DDL, and [TimePattern], the CHECK a stored timestamp is matched
// against. id.go holds [NewID], which mints an identifier under a per-table
// prefix. time.go holds [TimeLayout] and the [FormatTime], [Now], and
// [ParseTime] that go with it.
//
// This package owns no table. A package that owns one composes [Columns] and
// [Constraints], so the actor field and its constraints are written once and
// every record table carries the same ones.
//
// A link from one record to another is an id column — item.intent_id,
// release.build_id — and the package that owns one refuses an empty value
// twice: a CHECK named <column>_present in its own DDL, and the writer that
// sets the column. That text is per table rather than composed into
// [Constraints], because the column names differ per table. What the pair
// costs, stated here once for the whole graph: there are no foreign keys
// between record tables, so a link is checked for being present and never for
// pointing at a record that exists, and an id naming nothing is stored.
//
// Who may write what: nothing here writes to the database. The actor is
// supplied by the component that decided something, validated by
// [Actor.Validate] before a writer stores it, and validated again by the CHECK
// constraints in [Constraints], so a writer that skips the method is still
// refused.
//
// What defines it: the actor on every gate decision, edit, approval, and undo
// of a shipped change is seam 1 of "Security comes last",
// ../../end-goal/deferred.md#security-comes-last.
package record
