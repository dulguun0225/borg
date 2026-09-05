package people

import "github.com/dulguun0225/borg/factory/record"

// Table is the holding table: which duty or obligation a per-person key
// holds. HoldingIDPrefix is what [record.NewID] is called with for a row of
// it.
const (
	Table           = "people_holding"
	HoldingIDPrefix = "ppl"
)

// MappingTable is the key-to-name mapping, outside the chain. MappingIDPrefix
// is what [record.NewID] is called with for a row of it.
const (
	MappingTable    = "people_mapping"
	MappingIDPrefix = "pplmap"
)

// FormatVersion and MappingFormatVersion are written into format_version on
// every insert into [Table] and [MappingTable] respectively.
const (
	FormatVersion        = "people_holding/1"
	MappingFormatVersion = "people_mapping/1"
)

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than
// restated.
//
// one_holding is the rule that a row names a duty or an obligation and never
// both, written as a CHECK because it is the whole shape of the record: duty
// is zero exactly where obligation is set. The duty range is one to twelve,
// which is the twelve of what-humans-do.md and is why nothing here may write
// a thirteenth.
//
// credential_account and spend_ceiling are the lent credential's account and
// ceiling — ../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md's "the account is a
// person's own or an organisation's" and "the spend ceiling an owner may
// author beside a lent credential". Both are here as the empty-string and
// zero sentinels the rest of this package's columns already use rather than
// SQL NULL, because nothing in this milestone writes them: the fleet that
// would lend a credential is not built, so there is no caller to hand a
// value to these columns and no writer method that sets them yet. A rate per
// kind of unit is not a column at all for the same reason and is a bigger
// gap than a scalar: what it costs is that this table cannot yet carry what
// [policy.PersonDeclaration]'s Rates field already has room for.
//
// The unique constraint is over the key plus both holding columns rather
// than the key and one of them, so declaring the same holding twice is one
// row — the pair [Writer.Declare]'s insert conflicts on.
//
// actor_is_a_human is here as well as in the writer, the mirror of the
// incident record's actor_is_a_component: distributing the twelve is the
// owner's, and a component doing it would be the factory deciding who holds
// the factory's obligations.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	person_key text not null,
	duty int not null,
	obligation text not null,
	credential_account text not null,
	spend_ceiling double precision not null,
	withdrawn_at text not null,
	` + record.Constraints + `,
	constraint actor_is_a_human check (actor_kind = 'human'),
	constraint person_key_present check (person_key <> ''),
	constraint spend_ceiling_not_negative check (spend_ceiling >= 0),
	constraint one_holding check (
		(duty between 1 and 12 and obligation = '')
		or (duty = 0 and obligation in ('hosting', 'driftdetector', 'fleet'))
	),
	constraint one_row_per_key_and_holding unique (person_key, duty, obligation),
	constraint withdrawn_at_is_time_layout check (withdrawn_at = '' or withdrawn_at ~ '` + record.TimePattern + `')
)`,

	// The mapping is a record like any other — it still carries an actor and
	// a time — but it is not versioned through the chain: this table is what
	// an erasure deletes, and it is deleted in place rather than kept
	// withdrawn, unlike the holding table above.
	//
	// hours_start, hours_end and zone are the hours a service pages within,
	// per ../../end-goal/how-the-factory-works/08-operations/07-pages.md, read against the human who installed
	// the drift detector or holds an obligation with no reachable hours of
	// its own — unbuilt today the way credential_account above is: nothing
	// writes them yet, and they are the empty-string sentinel until
	// something does.
	`create table if not exists ` + MappingTable + ` (
	` + record.Columns + `,
	person_key text not null,
	name text not null,
	hours_start text not null,
	hours_end text not null,
	zone text not null,
	` + record.Constraints + `,
	constraint mapping_person_key_present check (person_key <> ''),
	constraint mapping_name_present check (name <> ''),
	constraint one_row_per_key unique (person_key)
)`,
}
