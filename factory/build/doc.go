// Package build owns the build record: one per commit built, naming the item
// it was built for and the commit it was made from, written when the
// implementation stage finishes and never written again.
//
// The record exists to be pointed at — the encoding of each criterion runs
// against a checkout of it, the gate's open event names it, and the release
// made from it points back at it — so it carries the item and the commit and
// nothing else. What happened to the build is written where it happened, by
// the component it happened at. The record does not name where the build was
// made: the commit is enough to make the same build again.
//
// writer.go is [Build], [Writer] and [NewWriter] with [Writer.Create], the one
// write, and the reads [Get], [ForCommit] and [Newest]; schema.go is [Table],
// [IDPrefix] and [DDL]. The tests are db_test.go, every one of them against
// the database.
//
// One row per commit built is the store's unique constraint on (item_id,
// commit_hash). What it costs: a rebuild of the same commit gets no second
// record, so how many times a commit was built is not a fact this table
// holds. item_id is an id field and not a foreign key — a cross-package link
// is a field the link walk reads, and the store does not check it points at
// anything.
//
// Who may write what: [Writer.Create] inserts into build and updates and
// deletes nothing. The record has no update method, so written once is a
// property of the API and not a discipline of the callers.
//
// What defines it: the Implementation gate in
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/05-implementation/README.md,
// the first gate that decides over a build; and where the criterion's run
// happens, in
// ../../end-goal/how-the-factory-works/05-environments/04-what-the-candidate-environment-decides/README.md.
package build
