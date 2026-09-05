// Package safeguard owns the safeguard record: its subject, the parameter it
// binds, its direction, its bound, its routing, and its withdrawal — a second
// record naming it, and not a field flipped in place.
//
// A safeguard is a record rather than a field on the record its subject names,
// because one of the subjects is a contract element and that record's writer is
// the merge queue; what that costs is that every mechanism a safeguard binds
// runs a query by subject rather than reading a field on the record already in
// hand.
//
// safeguard.go holds the vocabulary and the calls. [SubjectKind] is what a
// safeguard is drawn on and [SubjectKinds] is the nine the design names: a
// stage, a service, a project, an area, a contract element (named by its
// contract's id and the element's name, so a safeguard outlives the element
// row rewritten at every version), a design-system component, the list of
// allowed predicate kinds (stored against the factory-wide settings record,
// having no record of its own), the report store, and the drift detector's
// last check. A kind outside the nine is refused with [ErrSubjectKindUnknown],
// and the record a subject names is not read: a subject nobody declared is
// stored. [Subject.Key] carries the value of a parameter's own key — the gate
// row for the risk threshold, the stage for the attempt limit, and so on —
// which the design keeps out of the subject kinds themselves; [Insert] checks
// it against the parameter's [gatepolicy.Definition.Key], required where one
// exists and refused where none does.
//
// [Bound] is what a safeguard bounds by, in whichever of three shapes its
// parameter takes — a number, a list of names, or a [Predicate] — one struct
// rather than three arguments, so that a caller cannot pass one shape where
// another belongs and the store's CHECK of at most one filled column has one
// place in the code that decides which. [Routing] is the duty or the named
// human a safeguard's rows route to, meaningful only where the direction adds
// a human at a gate; at most one of its two fields is set. [Safeguard] is the
// record as stored, and [Insert] reads the direction off the parameter's
// definition rather than taking it as an argument. schema.go is [Table],
// [WithdrawalTable], the two id prefixes and [DDL], whose CHECK lists the same
// nine subject kinds.
//
// withdrawal.go holds [Withdrawal], [InsertWithdrawal] and [ApproveWithdrawal]:
// a withdrawal is written pending and is not in force until a second write
// approves it, the way the gate row A safeguard's withdrawal decides one, held
// by a human always. That row is not built, so [Withdraw] combines the two
// writes as a stand-in; a caller acting for the row it will become.
// [Safeguard.Withdrawn] and the exclusion in [BySubjects] both read
// [WithdrawalTable] rather than a field of the safeguard, because a safeguard
// is never edited.
//
// Who may write what: [Writer] is Factory. [Insert], [InsertWithdrawal] and
// [ApproveWithdrawal] take a transaction; [Insert] is called by package policy
// inside the one that appends the policy version, so the safeguard and the
// version commit together or not at all. Nothing here deletes a row. Every
// mechanism a safeguard binds reads through [BySubjects] and writes nothing;
// [All] is what the crude interface prints.
//
// What defines it: the one writer, the subjects, the routing field, a
// safeguard being a bound rather than a precedence, and the cost of the query
// are
// ../../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md.
// The withdrawal's gate row is
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/10-a-safeguards-withdrawal.md.
package safeguard
