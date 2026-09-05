// Package safeguard owns the safeguard record: its subject, the parameter it
// binds, its direction, its bound, and whether it has been withdrawn.
//
// A safeguard is a record rather than a field on the record its subject names,
// because one of the subjects is a contract element and that record's writer is
// the merge queue; what that costs is that every mechanism a safeguard binds
// runs a query by subject rather than reading a field on the record already in
// hand.
//
// safeguard.go holds the vocabulary and the calls. [SubjectKind] is what a
// safeguard is drawn on and [SubjectKinds] is the five this package stores — a
// service, an area, a gate row, the factory-wide settings record, and a contract
// element, which [Subject] names by its contract's id and the element's name so
// that a safeguard outlives the element row rewritten at every version. A kind
// outside the five is refused with [ErrSubjectKindUnknown], and the record a
// subject names is not read: a subject nobody declared is stored.
//
// [Bound] is what a safeguard bounds by, in whichever of three shapes its
// parameter takes — a number, a list of names, or a [Predicate] — one struct
// rather than three arguments, so that a caller cannot pass one shape where
// another belongs and the store's CHECK of at most one filled column has one
// place in the code that decides which. [Safeguard] is the record as stored, and
// [Insert] reads the direction off the parameter's definition rather than taking
// it as an argument. schema.go is [Table], [IDPrefix] and [DDL], whose CHECK
// lists the same five subject kinds.
//
// Who may write what: [Writer] is Factory. [Insert] and [Withdraw] take a
// transaction and are called by package policy inside the one that appends the
// policy version, so the safeguard and the version commit together or not at
// all. [Withdraw] marks the row and never deletes it. Every mechanism a
// safeguard binds reads through [BySubjects] and writes nothing; [All] is what
// the crude interface prints.
//
// What defines it: the one writer, the subjects, a safeguard being a bound
// rather than a precedence, and the cost of the query are
// ../../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md.
package safeguard
