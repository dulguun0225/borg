// Package people owns the People declaration: which of the owner's twelve
// duties each per-person key holds, and the named obligation a key holds
// outside the twelve. From the first record the identity is the key and
// never a name — [Mapping] is the one place a key maps to a name, kept
// outside the chain so an erasure can delete it alone.
//
// identity.go holds the vocabulary: a row names a [Duty] or an [Obligation]
// and never both, which is what [Holding] carries — [OfDuty] and
// [OfObligation] compose one. [Duties] is the twelve and [Obligations] the
// three: hosting the factory, installing the drift detector, and composing
// the fleet. schema.go holds [Table], [MappingTable], their id prefixes and
// [DDL].
//
// read.go holds [Declaration] and [Declaration.Holds], and the reads that
// take a pool and no [Writer]: [Get], [ByHolding], [Holders] — the
// notifier's read, returning keys — and [All], what the command-line interface
// prints. mapping.go holds [Mapping], [WriteMapping], [DeleteMapping],
// [GetMapping], [NameOf], the read every screen and every page event resolves
// a key through, and [KeyNamed], the same read the other way for a caller
// handed a name and holding no key.
//
// declare.go holds [Writer], [NewWriter], [Writer.Declare] and
// [Writer.Withdraw]. Every write to the holding table — a duty held or an
// obligation named — appends a policy version with People as caller before
// the table itself is written, the order every owner write to the
// declaration takes: the version first, the declaration second, so a stop
// between the two leaves a version naming what the table does not hold yet
// rather than the other way round. [Writer.versions] is a *[policy.Factory],
// composed in; a nil value appends no version. [Writer.Declare] is
// idempotent on the key and the holding, so declaring the same holding
// twice is one row, and [Writer.Withdraw] keeps the row and marks it, so a
// page delivered to a holder who has since stopped holding is still
// readable against the row that routed it. A key naming no holding at all
// is allowed — a [Mapping] with no row of [Table] behind it — added so a
// human can read the four screens without gating, approving, or otherwise
// acting anywhere.
//
// rederive.go holds [Rederive], called at the factory's start: it rewrites
// every duty the newest policy version's declaration names that the table
// does not already hold standing, and appends no version of its own.
//
// Nothing enforces a duty's routing and nothing has to: a duty with no
// holder is not an error, and an empty table is a working factory. The one
// field this package enforces is nothing yet either — the spend ceiling
// the design gives a lent credential is columns on [Table]
// (credential_account, spend_ceiling) that nothing writes: the fleet that
// would lend a credential is not built, so there is no caller for either.
//
// Who may write what: [Writer] inserts a holding and withdraws it, and it
// refuses an actor that is not a human with [ErrNotAnOwner]. [WriteMapping]
// and [DeleteMapping] are the mapping's only writer, also refusing a
// non-human actor; [DeleteMapping] takes a caller-supplied check for
// whether a legal hold reaches a record the key is written on, because this
// package holds no join from a key to the records that name it — that walk
// is the erasure list's, and it is not built, so a nil check never refuses.
// A withdrawal of a legal hold, once built, is what would call it with one.
//
// What defines it: the twelve duties and the three obligations outside them
// are ../../end-goal/what-humans-do.md; the record, the per-person key, the
// key-to-name mapping, and the version every write but the mapping's
// appends are
// ../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md;
// what reads it is
// ../../end-goal/how-the-factory-works/08-operations/07-pages.md, where the
// notifier routes all three channels on it; the mapping's deletion refused
// under a legal hold is
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/03-a-legal-hold.md;
// the opaque per-person key and the claimed-or-verified basis beside it are
// seam 1 of ../../end-goal/deferred.md#security-comes-last.
package people
