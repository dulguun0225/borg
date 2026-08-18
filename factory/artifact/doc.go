// Package artifact is the artifact store: one entrance for every artifact,
// the version chain and the authorship attribute on each, and the one call a
// spec version and the criteria it introduces are submitted in.
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
// record names it.
//
// # The store is the criterion's writer
//
// [Store.SubmitSpec] writes the artifact row and each criterion the version
// introduces through [criterion.Insert], in one transaction, so the spec and
// its criteria commit together or not at all — a draft the criterion package
// refuses takes the artifact row down with it. That is the one
// record-to-record import in the factory: this package imports
// [criterion] because it is that record's one writer, and every other link
// between record packages is an id field.
//
// The item_id column, and the service_id [Store.SubmitSpec] passes through
// to the criteria, are id fields and not foreign keys, which is the rule for
// every link between record packages. The store checks each for being present
// and not for pointing at anything; record's doc.go states that rule and its
// cost once.
//
// What defines it: the store, its three callers, and the version chain are
// the "One entrance for every artifact" arrangement in
// ../../end-goal/how-humans-do-it/01-one-pipeline.md; the one call a spec
// version and its criteria are submitted in is
// ../../end-goal/how-humans-do-it/03-gates.md#spec.
package artifact
