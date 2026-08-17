// Package record holds the conventions every record in the graph follows: the
// actor that wrote it, its identifier, the time it was written, and the SQL
// text a table composes so that no table restates them.
//
// It owns no table. A package that owns one composes [Columns] and
// [Constraints] into its own DDL, so the actor field and its constraints are
// written once and every record table carries the same ones.
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
