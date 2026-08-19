// Package pin owns the pin record: one record with one writer, Factory,
// naming its subject, the parameter it binds, its direction, its bound, and
// whether it has been withdrawn.
//
// A field on the record its scope names was the alternative, and it works for
// five subjects and breaks for two — a pinned predicate's subject is a contract
// element whose writer is the merge queue, and a pinned maximum age on the
// reconciler's last comparison would have the factory writing into a store it
// may never write. So a pin is a record, five readers and one writer, which is
// the shape the release record already has. What it costs is that what applies
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
// element, gate policy's own list, and the reconciler's last comparison. Four
// of those have a record at this milestone: a service, an area, a gate row, and
// the factory policy record. A project, a contract element, and the
// reconciler's last comparison are refused with [ErrSubjectKindUnknown],
// because storing a pin against a record kind that does not exist is storing a
// bound nothing can ever apply.
//
// The gate row is where the design's stage is read as the gate at that stage's
// boundary. Every one of the eight rows sits at one, a pin is what puts a human
// at a gate, and the two deploy rows are not stages of their own — so a pin
// naming a row is the only subject that reaches them.
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
