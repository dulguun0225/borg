package consumercontract

import "github.com/dulguun0225/borg/factory/record"

// Table is the predicate table this package owns: one row per predicate a
// consumer's build declares.
const Table = "consumer_contract"

// DerivationTable is the derivation table this package owns: one row per consumer
// contract version, naming the extractor that produced it and what that extractor
// could not follow.
const DerivationTable = "consumer_contract_derivation"

// IDPrefix is what [record.NewID] is called with for a predicate.
const IDPrefix = "cc"

// DerivationIDPrefix is what [record.NewID] is called with for a derivation.
const DerivationIDPrefix = "ccd"

// FormatVersion is what this package writes into format_version on every
// insert into [Table].
const FormatVersion = "consumer_contract/1"

// FormatVersionDerivation is what this package writes into format_version on
// every insert into [DerivationTable].
const FormatVersionDerivation = "consumer_contract_derivation/1"

// causes is the cause CHECK's value list. TestDDLListsEveryCause fails if it
// stops agreeing with [Causes].
const causes = `('no_extractor', 'extraction_failed')`

// DDL is this package's schema. [record.Columns] and [record.Constraints] are
// composed rather than restated, so the actor field and its constraints are the
// same ones every record table carries.
//
// The predicate table's unique constraint is on the consumer contract version and
// the whole of what the predicate asserts, so one version cannot introduce the
// same assertion twice — which a derivation that read one field through two paths
// would otherwise produce. It is not on the item: an item authors as many consumer
// contract versions as its stage was attempted, and each version's predicates
// stand beside the previous version's the way every artifact version does.
//
// item_id, service_id, artifact_id, and producer_service_id are id fields and not
// foreign keys, the rule record's doc.go states once. producer_service_id is the
// one that is allowed to be empty, and it means something: the name the consumer's
// build gave resolved to no service record, which is a consumer declaring against
// an interface the factory has never seen published.
//
// address is the entry of the consumer's configuration file the call site reads
// its address from, and it is required: which producer a call site reaches is the
// pairing of the site with that entry, and a predicate without it would be an edge
// nothing derived. An address the extractor cannot trace to an entry is not a
// predicate at all — it is a could-not-derive record, which is the row in
// [DerivationTable].
//
// The kind column has no CHECK listing values, which every other enumerated
// column in the factory has. The list of allowed predicate kinds a consumer
// contract draws from grows with the rest of gate policy — an owner extends it
// and a safeguard adds to it — so a list in the store would be a second place
// that list lives and would refuse a kind an owner had added. What refuses a
// kind this factory cannot decide is the derivation, which is where the design
// puts it.
//
// There is no column for the release the predicate entered force at, and there is
// no withdrawal column. Which release it entered force at is reached through the
// release record naming the same item; withdrawal is the next release deriving
// nothing, which is what makes the deprecation list unable to go stale.
//
// The derivation table is one row per consumer contract version, and the unique
// constraint says so. It carries what the record has to say about the derivation
// itself: the extractor, its version, the toolchain and the factory version that
// shipped it, the constructs it could not follow one per line, and the cause where
// it could not run at all. A row with a cause and no predicates beside it is could
// not derive; a row whose unfollowed list is not empty is partial; a row with
// neither is complete.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	item_id text not null,
	service_id text not null,
	artifact_id text not null,
	address text not null,
	producer_service text not null,
	producer_service_id text not null,
	interface_name text not null,
	element text not null,
	kind text not null,
	argument text not null,
	` + record.Constraints + `,
	constraint item_id_present check (item_id <> ''),
	constraint service_id_present check (service_id <> ''),
	constraint artifact_id_present check (artifact_id <> ''),
	constraint address_present check (address <> ''),
	constraint producer_service_present check (producer_service <> ''),
	constraint interface_name_present check (interface_name <> ''),
	constraint element_present check (element <> ''),
	constraint kind_present check (kind <> ''),
	constraint one_assertion_per_version unique (artifact_id, address, interface_name, element, kind, argument)
)`,

	`create table if not exists ` + DerivationTable + ` (
	` + record.Columns + `,
	item_id text not null,
	service_id text not null,
	artifact_id text not null,
	extractor text not null,
	extractor_version text not null,
	toolchain text not null,
	factory_version text not null,
	unfollowed text not null default '',
	cause text not null default '',
	reported text not null default '',
	` + record.Constraints + `,
	constraint item_id_present check (item_id <> ''),
	constraint service_id_present check (service_id <> ''),
	constraint artifact_id_present check (artifact_id <> ''),
	constraint toolchain_present check (toolchain <> ''),
	constraint an_extractor_or_a_cause check (extractor <> '' or cause <> ''),
	constraint cause_known check (cause = '' or cause in ` + causes + `),
	constraint reported_only_on_a_failure check (reported = '' or cause = 'extraction_failed'),
	constraint one_derivation_per_version unique (artifact_id)
)`,

	`create index if not exists consumer_contract_by_item on ` + Table + ` (item_id)`,
	`create index if not exists consumer_contract_by_producer on ` + Table + ` (producer_service_id, interface_name, element)`,
	`create index if not exists consumer_contract_derivation_by_item on ` + DerivationTable + ` (item_id)`,
}
