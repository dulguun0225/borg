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
// safeguard.go holds the vocabulary and [Writer.Insert]; query.go holds the
// reads, [BySubjects] and [All]. [SubjectKind] is what a safeguard is drawn
// on and [SubjectKinds] is the nine the design names: a
// stage, a service, a project, an area, a contract element (named by its
// contract's id and the element's name, so a safeguard outlives the element
// row rewritten at every version), a design-system component, the list of
// allowed predicate kinds (stored against the factory-wide settings record,
// having no record of its own), the report store, and the drift detector's
// last check. A kind outside the nine is refused with [ErrSubjectKindUnknown],
// and the record a subject names is not read: a subject nobody declared is
// stored. [Subject.Key] carries the value of a parameter's own key — the gate
// row for the risk threshold, the stage for the attempt limit, and so on —
// which the design keeps out of the subject kinds themselves; [Writer.Insert]
// checks it against the parameter's [gatepolicy.Definition.Key], required
// where one exists and refused where none does.
//
// [Bound] is what a safeguard bounds by, in whichever of three shapes its
// parameter takes — a number, a list of names, or a [Predicate] — one struct
// rather than three arguments, so that a caller cannot pass one shape where
// another belongs and the store's CHECK of at most one filled column has one
// place in the code that decides which. Two parameters take no bound at all: a
// safeguard on the risk threshold adds a human, and one on the rollout
// strategy's default keeps a control, which of the two strategies is the only
// one that adds anything. [Routing] is the duty or the named
// human a safeguard's rows route to, meaningful only where the direction adds
// a human at a gate; at most one of its two fields is set. [Safeguard] is the
// record as stored, and [Writer.Insert] reads the direction off the
// parameter's definition rather than taking it as an argument. schema.go is
// [Table], [WithdrawalTable], the two id prefixes and [DDL], whose CHECK
// lists the same nine subject kinds.
//
// withdrawal.go holds [Withdrawal], [Writer.InsertWithdrawal],
// [Writer.ApproveWithdrawal] and [GetWithdrawal], one withdrawal by id, which
// the gate row that decides it reads before it fires — the actor on that
// record is the one human the row may not route to, and the safeguard it
// names is what the row's routing is read from; and
// [WithdrawalsAwaitingADecision], every withdrawal no row has approved yet,
// which is what Factory lists as the rows outside every item pending a
// disposition:
// a withdrawal is written pending and is not in force until a second write
// approves it, the way the gate row A safeguard's withdrawal decides one, held
// by a human always. Nothing here combines the two writes — the row is what
// puts a human between them, and it is reached through
// policy.Factory.WriteSafeguardWithdrawal and
// policy.Factory.ApproveSafeguardWithdrawal.
// [Safeguard.Withdrawn] and the exclusion in [BySubjects] both read
// [WithdrawalTable] rather than a field of the safeguard, because a safeguard
// is never edited.
//
// Who may write what: [Writer] is Factory, and [Writer.Insert],
// [Writer.InsertWithdrawal] and [Writer.ApproveWithdrawal] are its methods —
// the record's one writer, reached through no other path, the writer holding
// the lease token every write is fenced with. Each takes a transaction;
// [Writer.Insert] is called by package policy inside the one that appends the
// policy version, so the safeguard and the version commit together or not at
// all. Nothing here deletes a row. Every mechanism a safeguard binds reads
// through [BySubjects] and writes nothing; [All] is what the command-line
// interface prints.
//
// What defines it: the one writer, the subjects, the routing field, a safeguard
// being a bound rather than a precedence, and the cost of the query are
// ../../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md
// (C2201, C2202, C2203, C2204, C2209, C2210, C2211, C2213, C2215, C2216, C2217).
//
// The withdrawal's gate row is
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/10-a-safeguards-withdrawal.md
// (C1196, C1205).
//
// A safeguard on an area putting a holder at decomposition is
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/README.md
// (C0790); a safeguard as how a human overrides the score is
// ../../end-goal/how-the-factory-works/03-gates/01-where-a-gate-is-and-what-decides-it.md
// (C0832); a safeguard raising the bake volume and never lowering it is
// ../../end-goal/how-the-factory-works/03-gates/02-the-rollout-strategy.md
// (C0927); a safeguard binding direction and bound per parameter is
// ../../end-goal/how-the-factory-works/08-operations/02-the-analysis-window.md
// (C2009).
//
// A safeguard on that row adding a human is
// ../../end-goal/how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md
// (C1420).
package safeguard
