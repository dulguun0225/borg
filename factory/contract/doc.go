// Package contract owns the contract record, the versions beside it, and the
// elements of each version — and, because nothing about a contract is declared,
// the vocabulary of a form, the diff between two forms, and the derivation of a
// form out of a checkout.
//
// # One published interface of one service
//
// A contract is one published interface or store of one service, identified by
// that service and the interface's own name in its build, with its [Kind] as the
// field everything else follows from. The kind is on the contract row and not on a
// version, which is what keeps it single-valued: two versions disagreeing about
// whether the thing is a store would enforce two promises on one interface, and
// that is the whole reason a contract is a record rather than a name for a service
// plus an interface name. [ErrKindChanged] is that rule at the write.
//
// What a contract promises follows from the kind and is declared nowhere. Every
// contract promises backward compatibility; a store promises forward as well,
// because its consumer is the service's own past — the release a rollback can
// restore. [Kind.Forward] is that derivation, and there is no third case: forward
// alone has no user, anything a rollback reads being read going the other way too.
//
// # Two versioned things, kept apart
//
// A release number orders the builds of one service. A contract version is a
// compatibility promise to whoever calls it, and semver is what that is for. One
// service publishes several contracts and they move at their own rates, so a
// single number per service could not express them. [Semver.Next] is the whole of
// how one moves: a major where the diff breaks, a minor where it does not, and a
// patch that never moves — nothing in a form is a patch-level change, and the third
// number is there because semver has three.
//
// # The merge queue is the writer
//
// [Publish] takes a [pgx.Tx] and its one caller is the merge queue, which calls it
// inside the transaction that mints the release's number. A contract changes only
// inside its service's items and every write to it happens at a release, so the
// fast-forward is the event; a writer of its own would be a component with one
// caller and the per-service ordering implemented again inside it, which is the
// argument that already put the release record and its number in the queue.
//
// Nothing is written onto the release. The version names the release and copies
// its number, so "the release names the contract versions it publishes" is the
// inbound edge every deploy record of a release already is —
// [VersionsForRelease] is that read. A column would be the same fact twice on a
// record that has no update, and package release's doc.go predicted one from M1
// until this milestone found it was not needed.
//
// The copied number is one fact in two places, taken for the reason package
// criterion takes the same cost with its item: the ordering of versions is the
// ordering of releases, and answering [VersionAt] by reading release records would
// make every reader of a contract a reader of releases. What makes the copy safe is
// that both rows are written by one writer inside one transaction, at one event.
//
// # The form is derived, and the derivation is one toolchain's
//
// [Derive] reads a checkout and returns what it publishes. How much of a build is
// visible is a property of its toolchain rather than of the factory, so derive.go
// is Go's and a second toolchain replaces it rather than extending it. The
// convention, the two file names it reads, the struct tag it reads, and what it
// cannot see are all stated there.
//
// [Diff] is what one form does to the one before it, with the elements whose
// change the promise does not allow already computed from the kind — a caller
// recomputing that would be a second place the rule lives. What [Diff] does not
// answer is who is affected: that is a query over the consumer contracts, and it
// belongs to whatever reads both.
//
// # Who may write what
//
// [Publish] inserts a contract, a version, and the version's elements, and
// updates and deletes nothing. Written once is a property of the API — there is no
// update method for a version or an element, and a form that moved is a new
// version rather than an edited one.
//
// service_id, release_id, and item_id are id fields and not foreign keys, and so
// are the two links inside this package. The store checks each for being present
// and not for pointing at anything; record's doc.go states that rule and its cost
// once.
//
// What defines it: the contract record, its kind, and the merge queue as its
// writer are ../../end-goal/how-the-factory-works/07-contracts/01-two-versioned-things.md;
// the promise each kind makes is
// ../../end-goal/how-the-factory-works/07-contracts/03-what-a-contract-promises.md and,
// for a store, ../../end-goal/how-the-factory-works/07-contracts/09-the-store-is-a-contract-too.md;
// the diff and what a breaking one is are
// ../../end-goal/how-the-factory-works/07-contracts/04-enforcement.md; the deprecation
// mark being derived from the build and minting a minor is
// ../../end-goal/how-the-factory-works/07-contracts/08-deprecation.md; and the unit
// belonging to an element's name is
// ../../end-goal/how-the-factory-works/07-contracts/05-what-a-diff-cannot-see.md.
package contract
