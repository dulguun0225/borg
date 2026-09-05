package agentrun

import "github.com/dulguun0225/borg/factory/record"

// Table is the one table this package owns.
const Table = "agent_run"

// IDPrefix is what [record.NewID] is called with for an agent run record.
const IDPrefix = "ar"

// FormatVersion is what this package writes into format_version on every
// insert into [Table].
const FormatVersion = "agent_run/1"

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than restated,
// so the actor field and its constraints are the same ones every record table
// carries.
//
// The columns are the design's four groups. What ran: role, the role prompt
// version in force, the skill versions the run matched, the model version, and
// the effort. What it ran on: the credential name, the processing location it
// resolved to, the per-person key of whoever lent it, and whether that account
// is a person's own or an organisation's. What it served: the item and its
// stage, or the intent, and the input manifest the run was handed. What it
// spent: the units the provider returned per kind, the time it returned them,
// the sources handed over, the rates the amount was converted at, and the
// converted amount.
//
// item_id, intent_id, input_manifest_id, role_prompt_version_id and the ids in
// skill_version_ids are id fields and not foreign keys, like every link between
// records; record's doc.go states that rule and its cost once.
//
// units_by_kind and rates_by_kind are JSON objects keyed by the kind a provider
// counts apart — input, output, cached input, or any other it returns — because
// which kinds exist is the provider's and not a list the factory may fix. A
// column per kind would be a schema edit per provider.
//
// converted_amount is numeric and nullable: it is absent where a kind the run
// returned has no rate, which is what makes a credential under a spend ceiling
// fail closed rather than sum an amount that is not there. The currency is the
// one the owner's rates are authored in and is empty exactly where the amount
// is absent.
//
// served_names_something is the design's "one of the five and never none",
// narrowed to the two a record exists for: an item or an intent. The grouper
// run and the evaluation-set run are the two of the five that name neither, and
// neither has a record yet — doc.go says so.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	role text not null,
	role_prompt_version_id text not null,
	skill_version_ids text not null,
	model_version text not null,
	effort text not null,
	credential_name text not null,
	processing_location text not null,
	lender_key text not null,
	account_kind text not null,
	item_id text not null,
	stage text not null,
	intent_id text not null,
	input_manifest_id text not null,
	units_by_kind text not null,
	units_at text not null,
	sources text not null,
	rates_by_kind text not null,
	converted_amount numeric,
	currency text not null,
	started_at text not null,
	finished_at text not null,
	outcome text not null,
	` + record.Constraints + `,
	constraint role_present check (role <> ''),
	constraint model_version_present check (model_version <> ''),
	constraint credential_name_present check (credential_name <> ''),
	constraint account_kind_known check (account_kind in ('', 'person', 'organisation')),
	constraint served_names_something check (item_id <> '' or intent_id <> ''),
	constraint stage_only_with_an_item check (stage = '' or item_id <> ''),
	constraint units_at_is_time_layout check (units_at ~ '` + record.TimePattern + `'),
	constraint started_at_is_time_layout check (started_at ~ '` + record.TimePattern + `'),
	constraint finished_at_is_time_layout check (finished_at ~ '` + record.TimePattern + `'),
	constraint outcome_present check (outcome <> ''),
	constraint currency_with_the_amount check (
		(converted_amount is null and currency = '') or (converted_amount is not null and currency <> '')
	)
)`,
}
