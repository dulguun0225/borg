package contract

import "github.com/dulguun0225/borg/factory/record"

// Table is the contract table this package owns: one row per published interface
// or store of one service.
const Table = "contract"

// VersionTable is the contract version table this package owns: one row per
// release that moved a contract's form.
const VersionTable = "contract_version"

// ElementTable is the element table this package owns: one row per element of
// one version's form.
const ElementTable = "contract_element"

// IDPrefix is what [record.NewID] is called with for a contract.
const IDPrefix = "con"

// VersionIDPrefix is what [record.NewID] is called with for a contract version.
const VersionIDPrefix = "conv"

// ElementIDPrefix is what [record.NewID] is called with for an element row. The
// identity of an element is its version and its name, so the id is the row's and
// never what anything points at — a safeguard's predicate names an element by
// the contract and the name, which [ElementSubject] composes, because the row's
// id changes at every version and a safeguard has to outlive one.
const ElementIDPrefix = "cone"

// kinds is the kind CHECK's value list, written once because doc.go's claim that
// the kind is single-valued across versions rests on it being on the contract row
// and nowhere else.
const kinds = `('interface', 'store')`

// elementKinds and positions are the element table's two CHECK value lists.
// TestDDLListsEveryElementKind and TestDDLListsEveryPosition fail if either stops
// agreeing with [ElementKinds] and [Positions].
const (
	elementKinds = `('field', 'operation', 'argument', 'message')`
	positions    = `('output', 'input', 'store')`
)

// FormatVersion is what this package writes into format_version on every
// insert into [Table].
const FormatVersion = "contract/1"

// FormatVersionVersion is what this package writes into format_version on
// every insert into [VersionTable].
const FormatVersionVersion = "contract_version/1"

// FormatVersionElement is what this package writes into format_version on
// every insert into [ElementTable].
const FormatVersionElement = "contract_element/1"

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than restated, so
// the actor field and its constraints are the same ones every record table
// carries.
//
// The unique constraint on (service_id, name) is what makes a contract one
// published interface of one service: a second row for one name would be two
// promises on one interface, and the kind being a column of this table rather
// than of a version is what keeps it single-valued across versions.
//
// A unique constraint's name has to be unique in the schema and not only in its
// table, because it creates an index and an index is a relation — which is not the
// rule record.go states for a CHECK. So one_version_per_semver is named for what it
// is rather than one_row_per_version, which package artifact already has.
//
// A version's release_number is copied from the release the same write mints, and
// the unique constraint on (contract_id, release_id) is what stops one merge
// publishing two versions of one contract. Copying the number is one fact in two
// places, taken for the reason package criterion takes the same cost with its
// item: the ordering of versions is the ordering of releases, and answering "the
// version in force at release N" by reading release records would make every
// reader of a contract a reader of releases. What makes the copy safe is that both
// rows are written by one writer inside one transaction, at one event.
//
// A version's item_id is empty on a release minted over an accepted commit, which
// names a build and no item: such a release is one no gate decided, and the
// version it publishes is keyed by the release rather than by the item. Every
// other version names the item its release was cut from.
//
// An element row carries what the diff reads: its kind — one field, one operation,
// one argument or one message — the position that says which way compatibility
// runs for it, the type as the build states it, whether it is required and whether
// it is always populated, the domain and the range it accepts, and the two other
// constraints a store element can carry. accepted_domain is empty for an element
// that accepts any name and range_low and range_high are null together for one
// that accepts any number, so a range appearing where there was none reads as the
// narrowing it is.
//
// service_id, release_id, and item_id are id fields and not foreign keys, which is
// the rule for every link between record packages. contract_id and
// contract_version_id are links inside this package and are checked the same way:
// record's doc.go states the present rule and its cost once, for every link column
// in the graph.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	service_id text not null,
	name text not null,
	kind text not null,
	` + record.Constraints + `,
	constraint service_id_present check (service_id <> ''),
	constraint name_present check (name <> ''),
	constraint kind_known check (kind in ` + kinds + `),
	constraint one_contract_per_service_and_name unique (service_id, name)
)`,

	`create table if not exists ` + VersionTable + ` (
	` + record.Columns + `,
	contract_id text not null,
	service_id text not null,
	release_id text not null,
	release_number bigint not null,
	item_id text not null default '',
	major int not null,
	minor int not null,
	patch int not null,
	` + record.Constraints + `,
	constraint contract_id_present check (contract_id <> ''),
	constraint service_id_present check (service_id <> ''),
	constraint release_id_present check (release_id <> ''),
	constraint release_number_positive check (release_number >= 1),
	constraint major_starts_at_one check (major >= 1),
	constraint minor_not_negative check (minor >= 0),
	constraint patch_not_negative check (patch >= 0),
	constraint one_version_per_release unique (contract_id, release_id),
	constraint one_version_per_semver unique (contract_id, major, minor, patch)
)`,

	`create table if not exists ` + ElementTable + ` (
	` + record.Columns + `,
	contract_version_id text not null,
	contract_id text not null,
	name text not null,
	kind text not null,
	element_position text not null,
	declared_type text not null,
	required boolean not null,
	populated boolean not null,
	deprecated boolean not null,
	accepted_domain text not null default '',
	range_low double precision,
	range_high double precision,
	not_null boolean not null default false,
	unique_rule boolean not null default false,
	` + record.Constraints + `,
	constraint contract_version_id_present check (contract_version_id <> ''),
	constraint contract_id_present check (contract_id <> ''),
	constraint name_present check (name <> ''),
	constraint element_kind_known check (kind in ` + elementKinds + `),
	constraint element_position_known check (element_position in ` + positions + `),
	constraint declared_type_present check (declared_type <> ''),
	constraint range_has_both_ends_or_neither check ((range_low is null) = (range_high is null)),
	constraint range_ends_in_order check (range_low is null or range_low <= range_high),
	constraint one_element_per_version_and_name unique (contract_version_id, name)
)`,

	`create index if not exists contract_version_by_contract on ` + VersionTable + ` (contract_id, release_number)`,
	`create index if not exists contract_element_by_version on ` + ElementTable + ` (contract_version_id)`,
}
