// Package artifact is the artifact store: one entrance for every artifact, the
// version chain and the authorship attribute on each, and the calls in which a
// version and the records it introduces are submitted together — a spec
// version with its criteria, its withdrawals and its screen state machines,
// and a consumer contract version with its predicates.
//
// # The files
//
// version.go is the vocabulary and the record: [Artifact], [Kind] with
// [Kinds], [ItemKinds] and [FleetKinds], [Authorship] with [Authorships],
// [EnteredBy] with [EnteredBys], [By] with [By.Empty], and every sentinel this
// package returns. writer.go is [Store] and [NewStore] with the item-kind
// submissions — [Store.SubmitSpec], [Store.SubmitPlan], [Store.SubmitTasks],
// [Store.SubmitImplementation], [Store.SubmitConsumerContract] — and
// insertVersion, which every submission goes through. fleet.go is
// [Store.SubmitFleet] and [Store.EnterShipped], the two calls that write a
// [FleetKinds] version. query.go is [Get], [Newest], [NewestShipped] and
// [InForce]. author.go is
// [NewestOfKind], [IDsByAuthor] and [ItemsByAuthor]. redact.go is [Span],
// [Store.Redact], [Store.RedactionPass] and [Store.Replay]. schema.go is
// [Table], [IDPrefix] and [DDL].
//
// The tests are db_test.go for the submissions and the chain,
// author_db_test.go for who authored a version and what it was authored from,
// and fleet_db_test.go for the fleet chains, the two shipped entries and the
// redaction; every one of them is against the database.
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
// kind: [KindSpec], [KindImplementationPlan], [KindTasks],
// [KindImplementation] and [KindConsumerContract] belong to
// an item; [KindRolePrompt] belongs to a role; [KindSkill] belongs to a
// subject — an area, a service, or a project; [KindSelectionRule] belongs to
// the factory as a whole and names none of the three. The store computes the
// next version inside the submitting transaction, and the unique constraint on
// (item_id, kind, role, subject, version) refuses the duplicate two concurrent
// submissions would produce, so the chain stays a chain without a lock.
//
// The content is the spec text for a spec, the plan's text for an
// implementation plan, one task per line for a tasks version, the commit hash
// for an implementation, the words a human reads the version by for a consumer
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
// [consumercontract.Insert], which writes the derivation that produced the
// version beside the predicates it introduces. Those are the record-to-record imports in the
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
// The two events are not one entry, and [EnteredBy] is which of them wrote the
// row: [EnteredByInstall]'s entries enter in force ungated and
// [EnteredByUpgradeFirstStart]'s enter awaiting the gate every version fires.
// Both write the same columns and either can write version 1 of a chain, so
// the column is what [InForce] reads and what keeps the caller from having to
// know which start wrote each row. The install step and the first-start step
// are the command-line interface's, and it makes both at every start: what
// decides whether either writes is [NewestShipped], the newest entry a start
// wrote, against the shipped-bundle identity this build carries.
//
// # In force
//
// [Newest] is the head of a fleet chain whatever decided it. [NewestShipped] is
// the head of what a start entered rather than anybody authored, which is what
// the first-start step reads: it carries the shipped-bundle identity it entered
// under, so a start under that identity enters nothing however many versions
// have been authored over it, and an upgrade whose words are the ones that entry
// carries enters nothing either. [InForce] is the newest version of a chain that is
// either among the version ids the caller names as approvedVersionIDs or an
// entry [EnteredByInstall] wrote — approval and withdrawal are the decision
// log's facts, which this package does not import, so the caller supplies them
// already combined, and the install's ungated entries are read off the row. It
// reads the chain by kind and role or subject,
// which is why it serves the three [FleetKinds] rather than an item-kind
// chain, whose "in force" question a criterion's or a machine's own in-force
// query answers instead.
//
// # Redaction
//
// [Store.Redact] is the one exception to "insert and never update": it
// destroys the named [Span]s of a version's content in place and recomputes
// content_digest, for erasure rather than correction. Its caller is
// [Store.RedactionPass], this store's own pass over the redactions naming the
// versions it writes, one of the three passes ../../end-goal/records.md gives
// a redaction's targets — a record package redaction writes and this one
// reads, so no component writes another's record. [Store.Replay] destroys
// again what the erasure list says was removed, which is what a restore is
// served through before this store serves anything: the list is outside the
// recovery unit and a backup taken before an erasure still carries the words.
//
// Neither write writes a row of that list: it has one writer and it is the
// report store, and the row for an artifact version is appended through that
// store by the erasure action at Factory, keyed by the key the action
// computed, before the redaction record exists and before anything here is
// called. So the row lands first and the record last, a step taken again
// appends nothing, and a stop leaves the event visibly owing. [Store.Replay]
// reads that list, which is a read and not a write. The content's length is
// unchanged by all of it, so the version chain and everything that names a
// version stand.
//
// # What a version was authored from
//
// Every submission takes the input manifest the run was handed as its last
// argument and writes it on the row. It is empty where the caller that
// dispatched the run wrote no manifest: context assembly, which the design has
// write one at every dispatch, is not built, so the component that dispatches
// an agent holds package inputmanifest's writer and supplies the id here. That
// caller is package dispatch.
// [Store.EnterShipped] writes none, an entry authoring nothing having read no
// manifest, and the DDL's input_manifest_only_when_authored CHECK refuses one.
//
// Who may write what: [Store]'s submissions and [Store.EnterShipped] insert an
// artifact version and the records that version introduces; [Store.Redact]
// updates content and content_digest and nothing else; nothing here deletes.
//
// What defines it: the store, its callers, and the version chain are the "One
// entrance for every artifact" arrangement in
// ../../end-goal/how-the-factory-works/01-one-pipeline.md (C0159, C0202, C0205,
// C0209, C0212, C0213, C0214, C0215, C0216, C0217, C0218, C0219, C0220, C0221,
// C0222); the one call a spec version, its criteria, its withdrawals and its
// machines are submitted in is
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/02-spec/README.md
// (C1100) and
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/02-spec/04-the-screen-state-machine.md
// (C1088); the role prompt, the skill, the selection rule, the version chain
// they share, and the ungated entry the factory writes are
// ../../end-goal/how-the-factory-works/10-fleet/03-what-an-agent-is-told/README.md
// (C2429, C2434, C2436, C2437, C2438, C2440, C2443, C2444, C2445, C2446,
// C2450).
//
// the pass that destroys what a redaction names and the replay after a restore
// are
// ../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/02-reports.md
// (C0447, C0448, C0456).
//
// Also ../../end-goal/records.md (C2799, C2801, C2802, C2803, C2804).
package artifact
