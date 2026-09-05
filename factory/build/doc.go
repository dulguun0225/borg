// Package build owns the build record: one per commit built, naming the
// service it was built for, the item where it has one, the commit, the
// artifact digest, the resolved set of third-party packages, the notice file
// derived from that set, the design system constraint in force where the
// project has screens, and what the build's own process decided about the
// criteria whose encodings declare it. Written once, when the build is
// performed, and never written again.
//
// The record exists to be pointed at — the encoding of each criterion runs
// against a checkout of it, the gate's open event names it, and the release
// made from it points back at it. What happened to the build beyond what it
// produced is written where it happened, by the component it happened at.
// The record does not name where the build was made: the commit is enough to
// make the same build again.
//
// writer.go is [ResolvedEntry], [Draft], [Build], [Writer] and [NewWriter]
// with [Writer.Create], the one write, and the reads [Get], [ForCommit] and
// [Newest] and [Resolved]; schema.go is [Table], [ResolvedTable], the two id prefixes, the
// two format versions, and [DDL]. The tests are db_test.go, every one of them
// against the database.
//
// # Item and service
//
// item_id is empty on a search build, which names a service and no item —
// the search's builds are of commits on no branch and decided by no gate.
// service_id is required on every build, item-bound or not. One row per
// commit built is the store's unique constraint on (item_id, service_id,
// commit_hash). What it costs: a rebuild of the same commit gets no second
// record, so how many times a commit was built is not a fact this table
// holds, and a rebuild is always a new build record rather than a second
// write to the first — there is no update method, so a re-verification or a
// candidate redeploy that needs a fresh build calls [Writer.Create] again.
// item_id and design_system_constraint_id are id fields and not foreign
// keys — a cross-package link is a field the link walk reads, and the store
// does not check either for pointing at anything.
//
// # The resolved set and its coverage
//
// [Draft.Resolved] is written to [ResolvedTable], one row per package: the
// ecosystem, the source it was resolved from, the package, the version, the
// digest of the content, the declared licence, and what required it. An entry
// whose resolver could not produce a digest, a licence, or the requiring edge
// carries that field empty, which is that entry's own coverage and not an
// error to fill in later. [Draft.ResolvedSetCoverage] is what the resolver
// read, per ecosystem, on the build itself, and
// [Resolved] reads that table back, which is what the merge queue compares the
// re-resolved set's digests against. [Draft.ResolvedSetCouldNotDerive] is the
// reason where resolution could not be performed at all — a record and not an empty set, because "nothing
// vulnerable was resolved" and "nothing resolved was visible" call for
// opposite responses. [Draft.NoticeFile] is produced from the same set in the
// same write.
//
// # What the build's own process decided
//
// [Draft.Results] is recorded through [criterion.InsertResults] inside the
// same transaction as the build row, as run 0 with [criterion.PlaceBuild] —
// the run and place [criterion.RecordResults] uses for a run on the candidate
// environment do not apply here, because this is the build's own process and
// not a later deploy, so [Writer.Create] calls [criterion.InsertResults]
// directly rather than wrapping it in a second transaction.
//
// # Callers
//
// Four: the implementation stage when it finishes, so that the Implementation
// gate decides over a build that exists and the score has a diff to compute
// the change factors from; the candidate deploy, where a rebuild is needed;
// the merge queue at re-verification, which builds the candidate branch onto
// the master it will actually merge into; and the search, whose builds are of
// commits on no branch and name the shipped-bundle identity of the release
// that made them rather than an item. None of the four is built yet; this
// package is written for all four callers to reach.
//
// Who may write what: [Writer.Create] inserts into [Table] and
// [ResolvedTable], and updates and deletes nothing. The record has no update
// method, so written once is a property of the API and not a discipline of
// the callers.
//
// What defines it: the Implementation gate in
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/05-implementation/README.md,
// the first gate that decides over a build; the build record, the resolved
// set, its coverage, the notice file, and the design system constraint field
// in
// ../../end-goal/how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md;
// what a build is called and the search's builds in
// ../../end-goal/how-the-factory-works/06-releases/03-what-a-build-is-called-and-when.md;
// the exposure list read from a diff and a build record in
// ../../end-goal/how-the-factory-works/04-risk-score/01-factors-at-least.md;
// the schema change a build declares and the double application the candidate
// environment performs on it in
// ../../end-goal/how-the-factory-works/07-contracts/09-the-store-is-a-contract-too.md;
// and where the criterion's run happens, in
// ../../end-goal/how-the-factory-works/05-environments/04-what-the-candidate-environment-decides/README.md.
package build
