package environment

import "github.com/dulguun0225/borg/factory/record"

// Table is the environment table this package owns.
const Table = "environment"

// CycleTable is the compose-and-reclaim cycle table this package owns: one row
// per time a candidate's environment was composed.
const CycleTable = "environment_cycle"

// ThresholdTable is the gate-threshold table this package owns: one row per
// environment and gate row an owner authored a threshold for.
const ThresholdTable = "environment_gate_threshold"

// IDPrefix is what [record.NewID] is called with for an environment.
const IDPrefix = "env"

// CycleIDPrefix is what [record.NewID] is called with for a cycle row.
const CycleIDPrefix = "ecy"

// ThresholdIDPrefix is what [record.NewID] is called with for a threshold row.
const ThresholdIDPrefix = "egt"

// FormatVersion is written into every environment record's format_version
// column. FormatVersionCycle and FormatVersionThreshold are written into every
// cycle row's and every threshold row's.
const (
	FormatVersion          = "environment/2"
	FormatVersionCycle     = "environment_cycle/1"
	FormatVersionThreshold = "environment_gate_threshold/1"
)

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than restated.
//
// The kind CHECK lists the three kinds, and TestDDLListsEveryKind is what says
// it and [Kinds] still name the same ones.
//
// The name is unique within the project, not within the install, because
// production is one record per project and every one of them is named
// [ProductionName]. one_production_per_project is the partial unique index that
// makes it one: a project with two production records is refused by the store
// and not by a reader noticing later.
//
// The project is required of a persistent kind and optional of a candidate's,
// the design making a candidate's environment the item's rather than the
// project's. [Candidates.Compose] requires one anyway, because
// [CountLiveCandidates] is scoped to a project and a candidate carrying none is
// counted against no ceiling.
//
// The platform is required of a persistent kind and empty on a candidate's: a
// candidate environment is composed on the platform the production environment
// record of the item's service declares, so the fact lives on that record and
// not on the candidate's. can_compose_on_demand is a fact about the platform an
// owner declares; [Insert] refuses a production environment whose platform
// cannot compose one, an environment per candidate being the shape the design
// admits and nothing else.
//
// max_concurrent_candidate_environments is authored on the production record
// alone. Nothing supplies a value for it, so zero is unauthored and not a
// ceiling of nothing — the CHECK is what keeps it off the other two kinds.
//
// Five columns are a candidate's and empty on a persistent kind, each with a
// constraint saying so rather than a comment. The item is the candidate's own,
// which is what item_id_matches_kind enforces in both directions: a persistent
// environment naming an item and a candidate's naming none are both refused.
// composed_from, seed_version and value_set_version are what the deployer put in
// place beside the candidate. torn_down_at and torn_down_reason are written
// together when the item merges, is dropped, or is superseded, and only a
// candidate is ever torn down for good; a reclamation closes a cycle and leaves
// both empty, the environment being composable again.
//
// withdrawn_at is the other end of a persistent environment's life and is a
// candidate's of nothing: an owner withdraws one at Factory, and a candidate's
// is torn down instead.
//
// A cycle row is one compose-and-reclaim cycle of one candidate's environment.
// one_open_cycle_per_environment is the partial unique index that makes a second
// composition of a live environment impossible: an environment has at most one
// cycle that has not been torn down. The converted amount is the environment-hour
// rate in force at the write applied to the span, fixed there and never repriced,
// so priced is false and both numbers are zero where no rate was in force.
//
// A threshold row exists only where an owner authored one: an absent row is the
// score supplying the value, which is why the column is not null here and the
// distinction is the row's presence rather than a null inside it. The unique
// constraint is what an authoring write conflicts on, so re-authoring a
// threshold is one row and not two.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	kind text not null,
	project_id text not null,
	name text not null,
	targets text not null,
	credential text not null,
	platform_name text not null,
	platform_credential text not null,
	can_compose_on_demand boolean not null,
	max_concurrent_candidate_environments integer not null,
	item_id text not null,
	composed_from text not null,
	seed_version text not null,
	value_set_version text not null,
	torn_down_at text not null,
	torn_down_reason text not null,
	withdrawn_at text not null,
	` + record.Constraints + `,
	constraint kind_known check (kind in ('production', 'customer', 'candidate')),
	constraint project_id_matches_kind check (kind = 'candidate' or project_id <> ''),
	constraint name_present check (name <> ''),
	constraint one_name_per_project unique (project_id, name),
	constraint targets_present check (targets <> ''),
	constraint credential_present check (credential <> ''),
	constraint platform_matches_kind check (
		(kind = 'candidate' and platform_name = '' and platform_credential = '' and can_compose_on_demand = false)
		or (kind <> 'candidate' and platform_name <> '' and platform_credential <> '')
	),
	constraint ceiling_is_productions check (kind = 'production' or max_concurrent_candidate_environments = 0),
	constraint ceiling_not_negative check (max_concurrent_candidate_environments >= 0),
	constraint item_id_matches_kind check ((kind = 'candidate') = (item_id <> '')),
	constraint composed_from_is_a_candidates check (kind = 'candidate' or composed_from = ''),
	constraint versions_are_a_candidates check (kind = 'candidate' or (seed_version = '' and value_set_version = '')),
	constraint torn_down_is_a_candidates check (kind = 'candidate' or (torn_down_at = '' and torn_down_reason = '')),
	constraint torn_down_at_is_time_layout check (torn_down_at = '' or torn_down_at ~ '` + record.TimePattern + `'),
	constraint torn_down_reason_matches_time check ((torn_down_at <> '') = (torn_down_reason in ('merged', 'dropped', 'superseded'))),
	constraint withdrawn_is_a_persistent_kinds check (kind <> 'candidate' or withdrawn_at = ''),
	constraint withdrawn_at_is_time_layout check (withdrawn_at = '' or withdrawn_at ~ '` + record.TimePattern + `')
)`,

	`create unique index if not exists one_production_per_project
	on ` + Table + ` (project_id) where kind = 'production'`,

	`create table if not exists ` + CycleTable + ` (
	` + record.Columns + `,
	environment_id text not null,
	began_at text not null,
	run_could_start_at text not null,
	torn_down_at text not null,
	reason text not null,
	priced boolean not null,
	rate_per_hour double precision not null,
	converted_amount double precision not null,
	` + record.Constraints + `,
	constraint environment_id_present check (environment_id <> ''),
	constraint began_at_is_time_layout check (began_at ~ '` + record.TimePattern + `'),
	constraint run_could_start_at_is_time_layout check (run_could_start_at = '' or run_could_start_at ~ '` + record.TimePattern + `'),
	constraint torn_down_at_is_time_layout check (torn_down_at = '' or torn_down_at ~ '` + record.TimePattern + `'),
	constraint reason_matches_teardown check ((torn_down_at <> '') = (reason in ('reclaimed', 'merged', 'dropped', 'superseded'))),
	constraint amount_matches_priced check (priced or (rate_per_hour = 0 and converted_amount = 0)),
	constraint amount_not_negative check (rate_per_hour >= 0 and converted_amount >= 0)
)`,

	`create unique index if not exists one_open_cycle_per_environment
	on ` + CycleTable + ` (environment_id) where torn_down_at = ''`,

	`create table if not exists ` + ThresholdTable + ` (
	` + record.Columns + `,
	environment_id text not null,
	gate_row text not null,
	threshold double precision not null,
	` + record.Constraints + `,
	constraint environment_id_present check (environment_id <> ''),
	constraint gate_row_present check (gate_row <> ''),
	constraint threshold_in_range check (threshold >= 0 and threshold <= 1),
	constraint one_row_per_environment_and_gate unique (environment_id, gate_row)
)`,
}
