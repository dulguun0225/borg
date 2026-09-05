package consumercontract

import "github.com/dulguun0225/borg/factory/record"

// Table is the one table this package owns: one row per predicate a consumer's
// build declares.
const Table = "consumer_contract"

// IDPrefix is what [record.NewID] is called with for a predicate.
const IDPrefix = "cc"

// FormatVersion is what this package writes into format_version on every
// insert into [Table].
const FormatVersion = "consumer_contract/1"

// DDL is this package's schema. [record.Columns] and [record.Constraints] are
// composed rather than restated, so the actor field and its constraints are the
// same ones every record table carries.
//
// The unique constraint is on the consumer contract version and the whole of what
// the predicate asserts, so one version cannot introduce the same assertion twice
// — which a derivation that read one field through two paths would otherwise
// produce. It is not on the item: an item authors as many consumer contract
// versions as its stage was attempted, and each version's predicates stand beside
// the previous version's the way every artifact version does.
//
// item_id, service_id, artifact_id, and producer_service_id are id fields and not
// foreign keys, the rule record's doc.go states once. producer_service_id is the
// one that is allowed to be empty, and it means something: the name the consumer's
// build gave resolved to no service record, which is a consumer declaring against
// an interface the factory has never seen published.
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
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	item_id text not null,
	service_id text not null,
	artifact_id text not null,
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
	constraint producer_service_present check (producer_service <> ''),
	constraint interface_name_present check (interface_name <> ''),
	constraint element_present check (element <> ''),
	constraint kind_present check (kind <> ''),
	constraint one_assertion_per_version unique (artifact_id, producer_service, interface_name, element, kind, argument)
)`,

	`create index if not exists consumer_contract_by_item on ` + Table + ` (item_id)`,
	`create index if not exists consumer_contract_by_producer on ` + Table + ` (producer_service_id, interface_name, element)`,
}
