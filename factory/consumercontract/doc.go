// Package consumercontract owns what a consumer's build declares about the
// contracts it reads and writes to: the predicates, the derivation that produced
// them, what deciding one means, and the Go extractor that reads both out of a
// checkout.
//
// # The files
//
// predicate.go is [Predicate] with [Predicate.Describe] and [Predicate.Validate],
// [Predicate.AgainstForm] and [Predicate.AgainstExchange] with the [Result] each
// returns, [Document], and [Domain] and [Range], the two arguments a kind reads.
// derivation.go is [Derivation] with [Derivation.Partial],
// [Derivation.CouldNotDerive] and [Derivation.Describe], [Derived], [Extractor],
// and [Cause] with [Causes]. writer.go is [Draft], [Of], [Insert] and
// [DeriveAgain]. derive.go is [Derive], [GoExtractor], [FileName] and
// [ErrNotAnAllowedPredicateKind]; address.go is [Entries] and [Entry], the
// configuration file that says which producer an address reaches; source.go is
// what the consumer's own source does with a mirror. read.go is [Querier] and the
// reads [Get], [ForArtifact], [ForItems], [AgainstProducer], [AgainstInterface],
// [NamingElement], [ItemsOf], [ConsumerServicesEver], [DerivationFor],
// [NewestDerivation] and [DerivationsForItems]. schema.go is [Table],
// [DerivationTable], the two id prefixes and [DDL].
//
// db_test.go is the tests against the database, and derive_test.go is the
// extractor, which needs a checkout and no database.
//
// A [Predicate] is drawn from the list of allowed predicate kinds, which is
// package gatepolicy's rather than this package's — the list is a parameter of
// gate policy, and two packages would otherwise each hold a copy of it. Five of
// the kinds are over what the consumer receives and four over what it sends, and
// what is here is what each means against something: [Predicate.AgainstForm]
// answers the seven a form can answer, including whether what the consumer sends
// stays inside what the form accepts, and [Predicate.AgainstExchange] answers the
// rest against [Document] values. A predicate the thing it was decided against
// could not answer comes back undecided rather than failed, and undecided is read
// at the gate the way a failure is.
//
// A [Derivation] is one row per consumer contract version: which extractor
// produced it, the toolchain and the factory version that shipped that extractor,
// the constructs it met and could not follow, and the cause where it could not run
// at all. A record whose list is empty is complete, one whose list is not is
// partial, and one with a cause is could not derive — a record and not an empty
// list, because "no consumer reads this" and "no consumer's read was visible" call
// for opposite responses.
//
// [Insert] is the one way a predicate and a derivation are written, and its one
// caller is the artifact store — the rows are written in the same call that
// submits the consumer contract version introducing them, which is why [Insert]
// takes a [pgx.Tx] rather than a pool. Package artifact importing this one is the
// second record-to-record import in the factory, taken for the reason the first
// was: the alternative is two writers of one table. [DeriveAgain] is the same
// write for the install's first-start step, which derives again at an upgrade that
// changed a toolchain's extractor; that step is not built, and this is what it
// calls.
//
// A predicate is written once and never updated, and there is no withdrawal
// column. A consumer that stops reading an element derives nothing at its next
// release, and what stops seeing the old assertion is the query over the range
// of releases in force. Which release a predicate entered force at is not here
// either — no release exists when a consumer contract is written — so that is
// reached through the release record naming the same item.
//
// [Derive], in derive.go, is the Go extractor: it reads the mirror the consumer
// holds of each address it reaches, pairs each with the entry of the configuration
// file that names which producer that address is, and derives from what the
// consumer's own source reads, writes and calls. The convention, the tag, and both
// blind cases — a read the derivation misses and a read it invents — are stated
// there and in source.go. It returns a [Derived], which [Insert] takes with an
// [Of].
//
// Who may write what: [Insert] inserts and updates and deletes nothing. item_id,
// service_id, artifact_id, and producer_service_id are id fields and not foreign
// keys, the rule record's doc.go states once — and producer_service_id is the one
// that may be empty, which means the name the consumer's build gave resolved to no
// service record.
//
// What defines it: the predicate, the allowed kinds on both sides, the
// derivation, its authority, and the two baselines are
// ../../end-goal/how-the-factory-works/07-contracts/06-what-a-consumer-declares.md; the
// consumer contract being checked against the candidate, and the third outcome,
// are ../../end-goal/how-the-factory-works/07-contracts/05-what-a-diff-cannot-see.md;
// what the record says about the derivation itself, and deriving again at an
// upgrade, are
// ../../end-goal/how-the-factory-works/07-contracts/12-what-the-derivation-records.md;
// which producer a consumer reaches is
// ../../end-goal/how-the-factory-works/07-contracts/11-which-producer-a-consumer-reaches.md;
// a store's consumer contract being derived from writes as well as reads is
// ../../end-goal/how-the-factory-works/07-contracts/09-the-store-is-a-contract-too.md;
// and the consumer contract version being an artifact of the item is
// ../../end-goal/how-the-factory-works/01-one-pipeline.md.
package consumercontract
