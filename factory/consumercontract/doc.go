// Package consumer contract owns the predicate a consumer's build declares about
// one element of one contract it reads: the record, the five kinds this factory
// can decide, what deciding one means, and the derivation of a consumer's
// predicates out of its checkout.
//
// # A predicate, not prose
//
// A consumer contract has to be decidable by the factory against one observed
// exchange, and it is drawn from a list of allowed predicate kinds the factory
// owns rather than invented at consumer contract time. The five kinds are
// package gatepolicy's, because the list they are the unauthored value of is a
// parameter of gate policy and two packages would otherwise each hold a copy of
// the list. What is here is what each one means against something:
// [Predicate.AgainstForm] and [Predicate.AgainstExchange].
//
// Those two are the design's two baselines, one level down. A form can answer
// three of the five — whether the element is there, whether it is always
// populated, and whether its name carries the unit — and an observed exchange can
// answer all five. A kind a form cannot answer comes back undecided rather than
// failed, which is not a pass: it is the assertion waiting for the side that can
// answer it, and the producer's own merge row is that side.
//
// # One writer, and it is another package
//
// [Insert] is the one way a predicate is written, and its one caller is the
// artifact store — the rows are written in the same call that submits the
// consumer contract version introducing them, so the version and its predicates
// cannot disagree about what was declared. That is why [Insert] takes a [pgx.Tx]
// rather than a pool. It is the second record-to-record import in the factory,
// package artifact importing this one, and it is taken for the reason the first
// was: the alternative is two writers of one table.
//
// A predicate is written once and never updated, and there is no withdrawal
// column. A consumer that stops reading an element derives nothing at its next
// release, and what stops seeing the old assertion is the query over the range of
// releases in force — which is what makes the deprecation list unable to go stale
// with nobody remembering to shorten it.
//
// # It is derived, and its authority is the gate's
//
// derive.go reads the consumer's checkout: the mirror it holds of each interface
// it reads, and which of that mirror's fields its own source actually selects. The
// convention, the struct tag, and both of the design's blind cases — a read the
// derivation misses and a read it invents — are stated there.
//
// Its authority does not come from the derivation being right. A derived consumer
// contract is an artifact of the consumer's item, ratified at the gates that item
// already passes; ratified that way it can reject a producer's candidate, and
// before that it rejects nothing, which is true of every artifact in the factory. A
// rejection later found wrong is a bad decision the score learns from.
//
// # Who may write what
//
// [Insert] inserts and updates and deletes nothing. item_id, service_id,
// artifact_id, and producer_service_id are id fields and not foreign keys, the
// rule record's doc.go states once — and producer_service_id is the one that may
// be empty, which means the name the consumer's build gave resolved to no service
// record.
//
// What is not here is which release a predicate entered force at. No release exists
// when a consumer contract is written, and the merge queue writing one later would
// be a second writer, so that is reached through the release record naming the same
// item — and the range of releases whose consumer contracts bind at a moment is a
// question about windows and numbers, which belongs to whatever reads both.
//
// What defines it: the predicate, the allowed kinds, the derivation, its
// authority, and the two baselines are
// ../../end-goal/how-humans-do-it/07-contracts.md#what-a-consumer-declares; the
// consumer contract being checked against the candidate is
// ../../end-goal/how-humans-do-it/07-contracts.md#what-a-diff-cannot-see; and
// the consumer contract version being an artifact of the item is
// ../../end-goal/how-humans-do-it/01-one-pipeline.md.
package consumercontract
