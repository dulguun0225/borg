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

// CredentialTable is the lent credential: who lent it, whether the account
// behind it is a person's own or an organisation's, and the spend ceiling
// authored on it. CredentialIDPrefix is what [record.NewID] is called with
// for a row of it.
const (
	CredentialTable    = "people_credential"
	CredentialIDPrefix = "pplcred"
)

// RateTable is the rate an owner authored per kind of unit a provider
// returns, per model version and effort, on one lent credential.
// RateIDPrefix is what [record.NewID] is called with for a row of it.
const (
	RateTable    = "people_rate"
	RateIDPrefix = "pplrate"
)

// FormatVersion, MappingFormatVersion, CredentialFormatVersion and
// RateFormatVersion are written into format_version on every insert into
// [Table], [MappingTable], [CredentialTable] and [RateTable] respectively.
const (
	FormatVersion           = "people_holding/1"
	MappingFormatVersion    = "people_mapping/1"
	CredentialFormatVersion = "people_credential/1"
	RateFormatVersion       = "people_rate/1"
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
	withdrawn_at text not null,
	` + record.Constraints + `,
	constraint actor_is_a_human check (actor_kind = 'human'),
	constraint person_key_present check (person_key <> ''),
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
	// A key and a name are the whole row. The hours a service pages within are
	// a field of the service record, per
	// ../../end-goal/how-the-factory-works/08-operations/07-pages.md, and a
	// wait naming no service pages at any hour, so no hours are held per
	// human here.
	`create table if not exists ` + MappingTable + ` (
	` + record.Columns + `,
	person_key text not null,
	name text not null,
	` + record.Constraints + `,
	constraint mapping_person_key_present check (person_key <> ''),
	constraint mapping_name_present check (name <> ''),
	constraint one_row_per_key unique (person_key)
)`,

	// The lent credential. The scope is the credential name: one credential
	// is one account at one provider and one invoice, so the row is unique on
	// the name and a key may lend more than one.
	//
	// account_kind is whether the account behind it is a person's own or an
	// organisation's, and it is required — the declaration holds it, and
	// [AccountKinds] lists the same two this CHECK does.
	//
	// ceiling_amount is nullable: where no ceiling is authored the credential
	// is unbounded. currency is the currency the owner's rates on this
	// credential are authored in and the one a ceiling is in. period_length
	// with period_unit is the length an owner authored, and
	// period_start_date with period_start_zone the start date and the zone it
	// was authored in, a period ending at that zone's midnight.
	// ceiling_authored_whole is what keeps a ceiling from standing without
	// the currency and the period it is compared over.
	`create table if not exists ` + CredentialTable + ` (
	` + record.Columns + `,
	person_key text not null,
	credential_name text not null,
	account_kind text not null,
	currency text not null,
	ceiling_amount numeric,
	period_length int not null,
	period_unit text not null,
	period_start_date text not null,
	period_start_zone text not null,
	withdrawn_at text not null,
	` + record.Constraints + `,
	constraint credential_actor_is_a_human check (actor_kind = 'human'),
	constraint credential_person_key_present check (person_key <> ''),
	constraint credential_name_present check (credential_name <> ''),
	constraint account_kind_known check (account_kind in ('person', 'organisation')),
	constraint period_unit_known check (period_unit in ('', 'day', 'month')),
	constraint period_length_not_negative check (period_length >= 0),
	constraint ceiling_amount_positive check (ceiling_amount is null or ceiling_amount > 0),
	constraint ceiling_authored_whole check (
		(ceiling_amount is null and currency = '' and period_length = 0
			and period_unit = '' and period_start_date = '' and period_start_zone = '')
		or (ceiling_amount is not null and currency <> '' and period_length > 0
			and period_unit <> '' and period_start_date <> '' and period_start_zone <> '')
		or (ceiling_amount is null and currency <> '' and period_length = 0
			and period_unit = '' and period_start_date = '' and period_start_zone = '')
	),
	constraint one_row_per_credential unique (credential_name),
	constraint credential_withdrawn_at_is_time_layout check (withdrawn_at = '' or withdrawn_at ~ '` + record.TimePattern + `')
)`,

	// The rate, per kind of unit the provider returns, per model version and
	// effort, on one lent credential. effort is empty where the provider
	// offers none, which is why it is in the unique constraint as a column
	// and not as a required value.
	`create table if not exists ` + RateTable + ` (
	` + record.Columns + `,
	credential_name text not null,
	unit text not null,
	model_version text not null,
	effort text not null,
	rate double precision not null,
	` + record.Constraints + `,
	constraint rate_actor_is_a_human check (actor_kind = 'human'),
	constraint rate_credential_name_present check (credential_name <> ''),
	constraint rate_unit_present check (unit <> ''),
	constraint rate_model_version_present check (model_version <> ''),
	constraint rate_not_negative check (rate >= 0),
	constraint one_row_per_rate unique (credential_name, unit, model_version, effort)
)`,
}
