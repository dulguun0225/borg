// Package contract owns the contract record, the versions beside it, and the
// elements of each version — and, because nothing about a contract is declared,
// the vocabulary of a form, the diff between two forms, and the derivation of a
// form out of a checkout.
//
// # The files
//
// contract.go is [Contract], [Version], and [ElementSubject]. form.go is
// [Kind] with [Kinds] and [Kind.Forward], [Element], and [Form] with
// [Form.Validate], [Form.Element], [Form.Marked] and [Form.Sorted]. diff.go is
// [Change] with [Change.Moved] and [Change.Describe], [Diff], and the version
// a contract moves on — [Semver], [FirstVersion], [Semver.Next],
// [ParseSemver]. derive.go is [Derive], [FileName], [TagWords] and
// [ErrDerivation]. writer.go is [Publication], [Published], [Publish] and
// [PublishAll]. read.go is [Querier] and the reads [Get], [ByName],
// [OfService], [All], [VersionAt], [NewestVersion], [VersionsOf],
// [VersionsForRelease], [ElementsOf] and [FormOf]. schema.go is [Table],
// [VersionTable], [ElementTable], the three id prefixes, and [DDL].
//
// db_test.go is the tests against the database, and derive_test.go is the
// derivation, which needs a checkout and no database.
//
// A contract is one published interface or store of one service, identified by
// that service and the interface's own name in its build. [Kind] is on the
// contract row and not on a version, which is what keeps it single-valued, and
// [ErrKindChanged] is that rule at the write. What a contract promises follows
// from the kind and is declared nowhere: [Kind.Forward] is that derivation.
//
// [Semver] is the version a contract moves on, apart from the release number
// that orders one service's builds. [Semver.Next] is the whole of how one
// moves: a major where the diff breaks, a minor where it does not, and a patch
// that never moves. [ParseSemver] reads one back.
//
// [Publish] and [PublishAll] take a [pgx.Tx] and their one caller is the merge
// queue, which calls them inside the transaction that mints the release's
// number — a contract changes only at a release, so a writer of its own would
// be a component with one caller.
//
// Nothing is written onto the release. The version names the release and copies
// its number, so [VersionsForRelease] reads what a release publishes as an
// inbound edge and [VersionAt] answers without making every reader of a
// contract a reader of releases. What makes the copy safe is that both rows are
// written by one writer inside one transaction.
//
// [Derive], in derive.go, reads a checkout and returns the forms it publishes.
// How much of a build is visible is a property of its toolchain, so derive.go
// is Go's and a second toolchain replaces it rather than extending it; the
// convention, the two file names it reads, the struct tag, and what it cannot
// see are stated there. [Diff] is what one form does to the one before it, with
// the elements whose change the promise does not allow already computed from
// the kind. Who is affected is not answered here: that is a query over the
// consumer contracts. [Get], [ByName], [OfService], [All], [VersionsOf],
// [NewestVersion], [ElementsOf], and [FormOf] are the reads.
//
// Who may write what: [Publish] inserts a contract, a version, and the
// version's elements, and updates and deletes nothing. Written once is a
// property of the API — there is no update method for a version or an element,
// and a form that moved is a new version rather than an edited one. service_id,
// release_id, item_id, and the two links inside this package are id fields and
// not foreign keys, checked for being present and not for pointing at anything;
// record's doc.go states that rule and its cost once.
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
