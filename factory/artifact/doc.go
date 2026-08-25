// Package artifact is the artifact store: one entrance for every artifact, the
// version chain and the authorship attribute on each, and the two calls in
// which a version and the records it introduces are submitted together — a spec
// version with its criteria, and a consumer contract version with its
// predicates.
//
// # One entrance
//
// Three things author a version of an artifact — the agent in the stage's
// role, a human backstopping that stage, and the gate component, where a
// human takes Edit in place — so the writer cannot be the position they
// occupy. [Store] is that writer, and the three are callers of it; the
// authorship column records which one called. What it costs is a dependency
// edge from every authoring agent and from the gate component back to this
// one package, and no way to hand the factory a spec written somewhere else
// without it passing through the same call.
//
// # The version chain
//
// A version is an int per item and kind, starting at 1, and each version
// names the id of the one it supersedes — the empty string for version 1.
// The store computes both inside the submitting transaction, and the unique
// constraint on (item_id, kind, version) refuses the duplicate two
// concurrent submissions would produce, so the chain stays a chain without a
// lock.
//
// For a spec the content is the spec text; for an implementation it is the
// commit hash the stage produced — the code lives in the repository, and the
// record names it; for a consumer contract it is the words a human reads the
// version by, the predicates being what the factory decides.
//
// A consumer contract is a third kind rather than a field of the implementation
// version. Both are derived from the same build at the same stage, and either can
// be authored again while the other stands — a re-derivation that finds the
// consumer reading one field fewer is a new consumer contract version and not a
// new implementation.
//
// # The store is the criterion's writer, and the predicate's
//
// [Store.SubmitSpec] writes the artifact row and each criterion the version
// introduces through [criterion.Insert], in one transaction, so the spec and its
// criteria commit together or not at all — a draft the criterion package refuses
// takes the artifact row down with it. [Store.SubmitConsumerContract] is the same
// arrangement with [consumercontract.Insert]: a version whose predicates were not
// written would be a consumer contract nothing can be decided against.
//
// Those are the two record-to-record imports in the factory, and both are here
// for one reason: this package is the one writer of both of those tables, and the
// alternative in each case is two writers of one table. Every other link between
// record packages is an id field.
//
// The item_id column, and the service_id [Store.SubmitSpec] and
// [Store.SubmitConsumerContract] pass through to the criteria and the
// predicates, are id fields and not foreign keys, which is the rule for every
// link between record packages. The store checks each for being present and not
// for pointing at anything; record's doc.go states that rule and its cost once.
//
// What defines it: the store, its three callers, and the version chain are
// the "One entrance for every artifact" arrangement in
// ../../end-goal/how-humans-do-it/01-one-pipeline.md; the one call a spec
// version and its criteria are submitted in is
// ../../end-goal/how-humans-do-it/03-gates/07-what-particular-gates-decide/02-spec.md.
package artifact
