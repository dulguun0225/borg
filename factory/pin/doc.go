// Package pin owns the pin record: one record with one writer, Factory,
// naming its subject, the parameter it binds, its direction, its bound, and
// whether it has been withdrawn.
//
// A field on the record its scope names was the alternative, and it works for
// five subjects and breaks for two — a pinned predicate's subject is a contract
// element whose writer is the merge queue, and a pinned maximum age on the
// reconciler's last comparison would have the factory writing into a store it
// may never write. So a pin is a record, five readers and one writer, which is
// the shape the release record already has. The first of those two is now built,
// and it is what [SubjectContractElement] and [Bound.Predicate] are. What it costs is that what applies
// at a mechanism is a query over pins by subject rather than a field read on the
// record already in hand, so every mechanism a pin binds does one more read.
//
// # The subject is not checked
//
// A pin naming a subject nobody declared is a dangling reference nothing
// detects until the mechanism looks for it. That is the design's own account of
// the cost and it is left as it stands: this package refuses a subject kind it
// has no record kind for, and does not read the record a subject names. The
// present rule package record states for a link column is the whole of what the
// store checks here too.
//
// # What a subject may be here
//
// The design admits a stage, a service, a project, an area, a contract
// element, gate policy's own list, and the reconciler's last comparison. Five
// of those have a record now: a service, an area, a gate row, the factory policy
// record, and a contract element — the last arriving with contracts, which is what
// makes a pinned predicate storable. A project and the reconciler's last
// comparison are refused with [ErrSubjectKindUnknown], because storing a pin
// against a record kind that does not exist is storing a bound nothing can ever
// apply.
//
// A contract element is named by its contract's id and the element's own name,
// which [contract.ElementSubject] composes. It is not the element row's id: that
// row is written afresh at every version, and a pin has to outlive one — an owner
// pins a predicate on an element and the producer keeps publishing versions of the
// contract it belongs to.
//
// The gate row is where the design's stage is read as the gate at that stage's
// boundary. Every one of the eight rows sits at one, a pin is what puts a human
// at a gate, and the two deploy rows are not stages of their own — so a pin
// naming a row is the only subject that reaches them.
//
// # Three shapes of bound
//
// [Bound] is what a pin bounds by, and a parameter takes one of three shapes: a
// number, a list of names, or one predicate. They are one struct rather than three
// arguments so that a caller cannot pass one shape where another belongs, and so
// that the store's own CHECK — at most one of the three columns filled — has one
// place in the code that decides which.
//
// A pinned predicate's bound is a [Predicate]: the kind and, where that kind takes
// one, its argument. What it is about is the pin's subject, which is the contract
// element, so the bound is the assertion and not the whole of it. The kinds are the
// same ones a derivation produces, because what a pin covers is a read the
// derivation could not see and the assertion about it is the ordinary one.
//
// Who may write what: [Writer] is Factory. [Insert] and [Withdraw] take a
// transaction and are called by package policy inside the one that appends the
// policy version, so the pin and the version commit together or not at all.
// Every mechanism a pin binds reads through [BySubjects] and writes nothing.
//
// What defines it:
// ../../end-goal/how-humans-do-it/09-gate-policy.md#one-shape-across-all-of-them,
// which sets the one writer, the subjects, a pin being a bound rather than a
// precedence, and the cost of the query.
package pin
