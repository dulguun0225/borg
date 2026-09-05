// Package artifact is the artifact store: one entrance for every artifact, the
// version chain and the authorship attribute on each, and the two calls in
// which a version and the records it introduces are submitted together — a spec
// version with its criteria, and a consumer contract version with its
// predicates.
//
// # The files
//
// writer.go is [Artifact] and [Draft], [Kind] with [Kinds], [Authorship] with
// [Authorships], [Store] and [NewStore] with the three submissions —
// [Store.SubmitSpec], [Store.SubmitImplementation],
// [Store.SubmitConsumerContract] — and [Get]. author.go is [By] — the
// authorship a version came through and the author a per-author prior is kept
// on — with the reads [NewestOfKind], [IDsByAuthor] and [ItemsByAuthor].
// schema.go is [Table], [IDPrefix] and [DDL].
//
// The tests are db_test.go, every one of them against the database.
//
// [Store] is the one writer and its three callers are the agent in the stage's
// role, a human backstopping that stage, and the gate component; the
// authorship column records which one called, so the writer is not the position
// they occupy.
//
// # The version chain
//
// A version is an int per item and kind, starting at 1, and each version names
// the id of the one it supersedes — the empty string for version 1. The store
// computes both inside the submitting transaction, and the unique constraint on
// (item_id, kind, version) refuses the duplicate two concurrent submissions
// would produce, so the chain stays a chain without a lock.
//
// The content is the spec text for a spec, the commit hash for an
// implementation, and the words a human reads the version by for a consumer
// contract. A consumer contract is a third kind rather than a field of the
// implementation version, so a re-derivation finding the consumer reads one
// field fewer is a new consumer contract version and not a new implementation.
//
// # The store is the criterion's writer, and the predicate's
//
// [Store.SubmitSpec] writes the artifact row and each criterion the version
// introduces through [criterion.Insert], in one transaction, so the spec and its
// criteria commit together or not at all. [Store.SubmitConsumerContract] is the
// same arrangement with [consumercontract.Insert]. Those are the two
// record-to-record imports in the factory, and both are here because this
// package is the one writer of both of those tables — the alternative in each
// case is two writers of one table.
//
// The item_id column, and the service_id [Store.SubmitSpec] and
// [Store.SubmitConsumerContract] pass through to the criteria and the
// predicates, are id fields and not foreign keys. The store checks each for
// being present and not for pointing at anything; record's doc.go states that
// rule and its cost once.
//
// Who may write what: [Store] inserts an artifact version and the records that
// version introduces, and updates and deletes nothing.
//
// What defines it: the store, its three callers, and the version chain are
// the "One entrance for every artifact" arrangement in
// ../../end-goal/how-the-factory-works/01-one-pipeline.md; the one call a spec
// version and its criteria are submitted in is
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/02-spec/README.md.
package artifact
