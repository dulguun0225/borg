// Package consumercontract owns the predicate a consumer's build declares
// about one element of one contract it reads: the record, what deciding one
// means, and the derivation of a consumer's predicates out of its checkout.
//
// # The files
//
// predicate.go is [Predicate] with [Predicate.Describe] and
// [Predicate.Validate], [Predicate.AgainstForm] and
// [Predicate.AgainstExchange] with the [Result] each returns, [Document], and
// [Domain] and [Range], the two arguments a kind reads. writer.go is [Draft],
// [Of] and [Insert]. derive.go is [Derive], [FileName] and [ErrDerivation],
// with [ErrNotAnAllowedPredicateKind] for an assertion outside the list.
// read.go is [Get], [ForArtifact], [ForItems], [AgainstProducer],
// [AgainstInterface], [NamingElement], [ItemsOf] and [ConsumerServicesEver].
// schema.go is [Table], [IDPrefix] and [DDL].
//
// db_test.go is the tests against the database, and derive_test.go is the
// derivation, which needs a checkout and no database.
//
// A [Predicate] is drawn from the list of allowed predicate kinds, which is
// package gatepolicy's rather than this package's — the list is a parameter of
// gate policy, and two packages would otherwise each hold a copy of it. What is
// here is what each kind means against something: [Predicate.AgainstForm]
// answers the three a form can answer, [Predicate.AgainstExchange] answers all
// five against [Document] values, and a kind a form cannot answer comes back
// undecided in the [Result] rather than failed.
//
// [Insert] is the one way a predicate is written, and its one caller is the
// artifact store — the rows are written in the same call that submits the
// consumer contract version introducing them, which is why [Insert] takes a
// [pgx.Tx] rather than a pool. Package artifact importing this one is the
// second record-to-record import in the factory, taken for the reason the first
// was: the alternative is two writers of one table.
//
// A predicate is written once and never updated, and there is no withdrawal
// column. A consumer that stops reading an element derives nothing at its next
// release, and what stops seeing the old assertion is the query over the range
// of releases in force. Which release a predicate entered force at is not here
// either — no release exists when a consumer contract is written — so that is
// reached through the release record naming the same item.
//
// [Derive], in derive.go, reads the consumer's checkout: the mirror it holds of
// each interface it reads, and which of that mirror's fields its own source
// actually selects. The convention, the struct tag, and both blind cases — a
// read the derivation misses and a read it invents — are stated there. It
// returns [Draft] values, which [Insert] takes with an [Of]. [Get],
// [ForArtifact], [ForItems], [AgainstProducer], [AgainstInterface],
// [NamingElement], [ItemsOf], and [ConsumerServicesEver] are the reads.
//
// Who may write what: [Insert] inserts and updates and deletes nothing.
// item_id, service_id, artifact_id, and producer_service_id are id fields and
// not foreign keys, the rule record's doc.go states once — and
// producer_service_id is the one that may be empty, which means the name the
// consumer's build gave resolved to no service record.
//
// What defines it: the predicate, the allowed kinds, the derivation, its
// authority, and the two baselines are
// ../../end-goal/how-the-factory-works/07-contracts/06-what-a-consumer-declares.md; the
// consumer contract being checked against the candidate is
// ../../end-goal/how-the-factory-works/07-contracts/05-what-a-diff-cannot-see.md; and
// the consumer contract version being an artifact of the item is
// ../../end-goal/how-the-factory-works/01-one-pipeline.md.
package consumercontract
