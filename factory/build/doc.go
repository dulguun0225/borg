// Package build owns the build record: one per commit built, naming the item
// it was built for and the commit it was made from, written when the
// implementation stage finishes and never written again.
//
// The record exists to be pointed at. Every pre-merge fact attaches to a
// build: the encoding of each criterion runs against a checkout of it, the
// gate's open event names it, and the release made from it points back at
// it. So the record carries the item and the commit and nothing else — what
// happened to the build is written where it happened, by the component it
// happened at.
//
// There is no candidate environment until M3, per
// ../../roadmap.md#m1--one-change-ships, so in M1 a criterion is decided by
// running its encoding wherever the build was made. The record does not say
// where that was: it names the commit, and the commit is enough to make the
// same build again.
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
// ../../end-goal/how-humans-do-it/03-gates/07-what-particular-gates-decide/05-implementation.md, the first gate
// that decides over a build — the build is made from the item's candidate
// branch when the stage finishes — and the M1 decisions in
// ../../roadmap.md#m1--one-change-ships, which put the criterion's run
// wherever the build was made until M3 builds the candidate environment.
package build
