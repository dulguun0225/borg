// Package principal is who a call is made as: the actor of seam 1 plus, for an
// agent, the dispatch it was put on an item by and the scope it was dispatched
// under.
//
// # The files
//
// principal.go holds [Principal] with [OfComponent], [OfHuman] and [OfAgent],
// [Principal.Validate] and its errors — [ErrDispatchOnlyOnAnAgent] and
// [ErrAgentNamesNoDispatch] — and [Principal.String], the one rendering a
// caller records beside what was asked for. principal_test.go is the whole of
// the tests; nothing here reaches a database.
//
// Who may write what: this package writes no record and reaches no store. A
// principal travels on a call and is recorded by whatever the call reaches —
// the resolver of seam 3 records it beside the name asked for, and each
// operation of seam 4 records it beside what was asked.
//
// It is a package of its own rather than a field of record.Actor because the
// two are different facts: the actor is who wrote a record, and the principal
// is who made the call. A record holds the first and never the second, and a
// seam takes the second and writes nothing.
//
// Nothing here verifies anything. A principal is populated, self-asserted, and
// enforced by nothing, the treatment the actor already gets, and the field on
// the factory-wide settings record that turns enforcement on is package
// factorysettings'.
//
// What defines it: a principal on every call is seam 5 of "Security comes
// last", ../../end-goal/deferred.md#security-comes-last. What an agent's scope
// is, and what a dispatch puts on an item, are
// ../../end-goal/how-the-factory-works/01-one-pipeline.md and
// ../../end-goal/how-the-factory-works/02-intent-into-items/05-dispatch.md.
package principal
