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
// [EnteredBy] with [EnteredBys], [By] with [By.Empty], [FactoryStart], and
// every sentinel this package returns. writer.go is [Store] and [NewStore]
// with the item-kind submissions — [Store.SubmitSpec], [Store.SubmitPlan],
// [Store.SubmitTasks], [Store.SubmitImplementation],
// [Store.SubmitConsumerContract] — [Store.DeriveConsumerContractAgain], the
// first-start step's derivation, and insertVersion, which every submission
// goes through. fleet.go is [Store.SubmitFleet] and [Store.EnterShipped], the
// two calls that write a [FleetKinds] version. query.go is [Get], [Newest], [NewestShipped],
// [InForce] and [ForItem]. author.go is
// [NewestOfKind], [IDsByAuthor] and [ItemsByAuthor]. redact.go is [Span],
// [Store.Redact], [Store.RedactionPass] and [Store.Replay]. schema.go is
// [Table], [IDPrefix] and [DDL].
//
// The tests are db_test.go for the submissions and the chain,
// author_db_test.go for who authored a version and what it was authored from,
// and fleet_db_test.go for the fleet chains, the two shipped entries and what
// only the factory's own start may write; every one of them is against the
// database. The erasure's are redact_db_test.go's.
//
// [Store] is the one writer, and its callers are the ones [Authorships] names
// — the agent in the stage's role, a human backstopping that stage, and the
// gate component — plus the factory's own start through [Store.EnterShipped]
// and [Store.DeriveConsumerContractAgain]; the authorship column records which
// one called, so the writer is not the position they occupy. A human
// backstops a stage, so [AuthorshipHuman] is an item kind's: the fleet kinds
// belong to no stage and [Store.SubmitFleet] refuses it, what a human writes
// into a role prompt, a skill or the selection rule being written at the gate
// under [AuthorshipGate].
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
// at the write and never recomputed; redacted_content_digest is the same over
// what a redaction left, and "Redaction" below says why both are kept.
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
// shippedBundleIdentity, the release of the product that entered it.
//
// The actor is [FactoryStart] and no other, on this call and on
// [Store.DeriveConsumerContractAgain]. Nothing else on such a row says who
// wrote it, so an actor the caller chose would let any component enter a
// version that nothing decided and that the install's half enters in force.
//
// [Store.DeriveConsumerContractAgain] is the same call for the one kind
// outside [FleetKinds] a start writes: at an upgrade's first start where the
// shipped extractor for a toolchain changed or was added, the first-start step
// derives a consumer contract again for every release in force on that
// toolchain and writes it beside the earlier record, never over it. It is
// [EnteredByUpgradeFirstStart]'s alone — an install has no release in force to
// derive over — and shippedBundleIdentity there is the release that shipped
// the extractor, a derivation being a function of the code and of the factory
// version. What is not built above this call is the step that finds the
// releases: which toolchain's extractor changed, and what each release's build
// derives to, are the first-start step's to read.
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
// The three reads here are one fleet chain's, and fleetKey is what says which:
// a kind outside [FleetKinds] is [ErrFleetKindUnknown], and the query names
// the chain's key and no item. Given an item kind and no chain key they would
// otherwise answer with the newest row of that kind across every item, which
// is no chain's head. What is in force on an item-kind chain is a criterion's
// or a machine's own in-force query, and the newest version of one is
// [NewestOfKind], which is keyed by the item.
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
// already combined, and the install's ungated entries are read off the row.
//
// # Redaction
//
// [Store.Redact] is the one exception to "insert and never update": it
// destroys the named [Span]s of a version's content in place, for erasure
// rather than correction. It leaves content_digest as it was — the digest of
// the words as they were written — and writes the digest over what remains
// into redacted_content_digest beside it. A gate's decision recorded the
// digest of the words it decided, and a digest recomputed in place would leave
// that decision naming nothing any row holds; the row carrying both is what
// keeps the decision naming what it decided and still says what the words are
// now.
//
// Its caller is [Store.RedactionPass], this store's own pass over the
// redactions naming the versions it writes, one of the three passes ../../end-goal/records.md gives
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
// argument and writes it on the row, and a version an agent authored names one
// or is refused, here and by the DDL's input_manifest_names_the_dispatch
// CHECK: a manifest is written at every dispatch, so an artifact authored from
// a truncated read does not pass for one authored from everything. Context
// assembly, which the design has write it, is not built, so the component that
// dispatches an agent holds package inputmanifest's writer and supplies the id
// here. That caller is package dispatch.
//
// A version a human wrote, at a stage or at a gate, was authored outside a
// dispatch and names none. [Store.EnterShipped] and
// [Store.DeriveConsumerContractAgain] write none either, a call authoring
// nothing having read no manifest, and the DDL's
// input_manifest_only_when_authored CHECK refuses one.
//
// Who may write what: [Store]'s submissions, [Store.EnterShipped] and
// [Store.DeriveConsumerContractAgain] insert an artifact version and the
// records that version introduces; [Store.Redact] updates content and
// redacted_content_digest and nothing else; nothing here deletes.
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
// the consumer contract the install's first-start step derives again at an
// upgrade that changed the extractor, written beside the earlier record and
// never over it, is
// ../../end-goal/how-the-factory-works/07-contracts/12-what-the-derivation-records.md
// (C1908, C1910).
//
// the pass that destroys what a redaction names and the replay after a restore
// are
// ../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/02-reports.md
// (C0447, C0448, C0456).
//
// what a rework request leaves in the version chain is
// ../../end-goal/how-the-factory-works/03-gates/06-going-back-up.md (C1030).
//
// Also ../../end-goal/records.md (C2799, C2801, C2802, C2803, C2804).
package artifact
