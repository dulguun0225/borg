// Package record holds the conventions every record in the graph follows: the
// actor that wrote it, its identifier, the time it was written, and the SQL
// text a table composes so that no table restates them.
//
// It owns no table. A package that owns one composes [Columns] and
// [Constraints] into its own DDL, so the actor field and its constraints are
// written once and every record table carries the same ones.
//
// # A link is checked for being present
//
// A link from one record to another is an id column — item.intent_id,
// release.build_id — and every package that owns one refuses an empty value
// twice: a CHECK named <column>_present in its own DDL, and the writer that
// sets the column. The text is per table rather than composed into
// [Constraints], because the column names differ per table.
//
// What the pair costs, and this is the one place it is stated: a link is
// checked for being present and never for pointing at a record that exists.
// There are no foreign keys between record tables, so an id naming nothing is
// stored without complaint and only a reader following the link finds out. An
// empty link is different in kind — it names nothing at all, and it is
// checkable without a foreign key — which is why the store refuses that much
// and no more.
//
// Who may write what: nothing here writes to the database. The actor is
// supplied by the component that decided something, validated by
// [Actor.Validate] before a writer stores it, and validated again by the CHECK
// constraints in [Constraints] so a writer that skips the method is still
// refused.
//
// What defines it: seam 1 of "Security comes last" in
// ../../end-goal/deferred.md#security-comes-last — an actor on every gate
// decision, edit, approval, and veto, populated from the first record, because
// identity cannot be added to a history written without it. There is no
// authentication and no enforcement behind the name: the field is the whole of
// the seam today.
package record
