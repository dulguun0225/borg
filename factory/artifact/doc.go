// Package artifact is the artifact store: one entrance for every artifact, the
// version chain and the authorship attribute on each, and the calls in which a
// version and the records it introduces are submitted together — a spec
// version with its criteria, its withdrawals and its screen state machines,
// and a consumer contract version with its predicates.
//
// # The files
//
// writer.go is [Artifact], [Kind] with [Kinds], [ItemKinds] and [FleetKinds],
// [Authorship] with [Authorships], [By] with [By.Empty], [Store] and
// [NewStore] with the item-kind submissions — [Store.SubmitSpec],
// [Store.SubmitImplementation], [Store.SubmitConsumerContract]. fleet.go is
// [Store.SubmitFleet] and [Store.EnterShipped], the two calls that write a
// [FleetKinds] version. query.go is [Get] and [InForce]. author.go is
// [NewestOfKind], [IDsByAuthor] and [ItemsByAuthor]. redact.go is [Span] and
// [Store.Redact]. schema.go is [Table], [IDPrefix] and [DDL].
//
// The tests are db_test.go, every one of them against the database.
//
// [Store] is the one writer, and its callers are the ones [Authorships] names
// — the agent in the stage's role, a human backstopping that stage, and the
// gate component — plus the factory's own start through [Store.EnterShipped];
// the authorship column records which one called, so the writer is not the
// position they occupy.
//
// # The version chain
//
// A version is an int per chain and kind, starting at 1, and each version
// names the id of the one it supersedes — the empty string for version 1. A
// chain is named by exactly one of item_id, role and subject, depending on the
// kind: [KindSpec], [KindImplementation] and [KindConsumerContract] belong to
// an item; [KindRolePrompt] belongs to a role; [KindSkill] belongs to a
// subject — an area, a service, or a project; [KindSelectionRule] belongs to
// the factory as a whole and names none of the three. The store computes the
// next version inside the submitting transaction, and the unique constraint on
// (item_id, kind, role, subject, version) refuses the duplicate two concurrent
// submissions would produce, so the chain stays a chain without a lock.
//
// The content is the spec text for a spec, the commit hash for an
// implementation, the words a human reads the version by for a consumer
// contract, and the role prompt, skill or selection rule text itself for a
// fleet kind. content_digest is the sha256 of content in hexadecimal, computed
// at the write.
//
// # The store is the criterion's writer, the machine's, and the predicate's
//
// [Store.SubmitSpec] writes the artifact row, each criterion the version
// introduces through [criterion.Insert], each criterion id it withdraws
// through [criterion.Withdraw], and each screen state machine it introduces or
// revises through [screenstatemachine.Insert], all in one transaction, so the
// spec, its criteria, its withdrawals and its machines commit together or not
// at all. [Store.SubmitConsumerContract] is the same arrangement with
// [consumercontract.Insert]. Those are the record-to-record imports in the
// factory, and each is here because this package is the one writer of every
// table it reaches — the alternative in each case is two writers of one
// table.
//
// The item_id column, and the service_id [Store.SubmitSpec] and
// [Store.SubmitConsumerContract] pass through to the criteria, the
// withdrawals and the machines, are id fields and not foreign keys. The store
// checks each for being present where required and not for pointing at
// anything; record's doc.go states that rule and its cost once.
//
// # The one entry nobody wrote
//
// [Store.EnterShipped] is the fourth call named in "One entrance for every
// artifact": at install, and at a first start on an upgrade that changed
// shipped words, the factory calls it to enter what shipped, with the
// factory's own start as the actor. It writes a [FleetKinds] version with
// [By.Empty] true — authorship and author both empty, the one pair the DDL's
// author_pair_together CHECK admits as a partial one — and names
// shippedBundleIdentity, the release of the product that entered it, present
// on this entry and on no other.
//
// # In force
//
// [InForce] is the newest version of a chain among the version ids the caller
// names as approvedVersionIDs — approval and withdrawal are the decision
// log's facts, which this package does not import, so the caller supplies
// them already combined. It reads the chain by kind and role or subject,
// which is why it serves the three [FleetKinds] rather than an item-kind
// chain, whose "in force" question a criterion's or a machine's own in-force
// query answers instead.
//
// # Redaction
//
// [Store.Redact] is the one exception to "insert and never update": it
// destroys the named [Span]s of a version's content in place and recomputes
// content_digest, for erasure rather than correction. Its caller, the report
// store's redaction pass, is not built.
//
// Who may write what: [Store]'s submissions and [Store.EnterShipped] insert an
// artifact version and the records that version introduces; [Store.Redact]
// updates content and content_digest and nothing else; nothing here deletes.
//
// What defines it: the store, its callers, and the version chain are the "One
// entrance for every artifact" arrangement in
// ../../end-goal/how-the-factory-works/01-one-pipeline.md; the one call a spec
// version, its criteria, its withdrawals and its machines are submitted in is
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/02-spec/README.md
// and
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/02-spec/04-the-screen-state-machine.md;
// the role prompt, the skill, the selection rule, the version chain they share,
// and the ungated entry the factory writes are
// ../../end-goal/how-the-factory-works/10-fleet/03-what-an-agent-is-told/README.md.
package artifact
