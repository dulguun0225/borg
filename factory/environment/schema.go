package environment

import "github.com/dulguun0225/borg/factory/record"

// Table is the environment table this package owns.
const Table = "environment"

// ThresholdTable is the gate-threshold table this package owns: one row per
// environment and gate row an owner authored a threshold for.
const ThresholdTable = "environment_gate_threshold"

// IDPrefix is what [record.NewID] is called with for an environment.
const IDPrefix = "env"

// ThresholdIDPrefix is what [record.NewID] is called with for a threshold row.
const ThresholdIDPrefix = "egt"

// FormatVersion is written into every environment record's format_version
// column. FormatVersionThreshold is written into every threshold row's.
const (
	FormatVersion          = "environment/1"
	FormatVersionThreshold = "environment_gate_threshold/1"
)

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than restated.
//
// The kind CHECK lists the two kinds this milestone writes; the third, one a
// customer defines, is
// ../../end-goal/how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md.
// The name is unique, so production is one record until a project record exists
// to have one each, and a candidate's name is derived from its item so two
// candidates cannot collide on one.
//
// Three columns are a candidate's and empty on a persistent kind, each with a
// constraint saying so rather than a comment. The item is the candidate's own,
// which is what item_id_matches_kind enforces in both directions: a persistent
// environment naming an item and a candidate's naming none are both refused.
// composed_from is what the deploy agent put in place beside the candidate, and
// is empty where it put nothing there — a candidate whose item declared no
// dependency. torn_down_at is written when the item merges, is dropped, or is
// superseded, and only a candidate is ever torn down.
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
	name text not null unique,
	targets text not null,
	credential text not null,
	item_id text not null,
	composed_from text not null,
	torn_down_at text not null,
	` + record.Constraints + `,
	constraint kind_known check (kind in ('production', 'candidate')),
	constraint name_present check (name <> ''),
	constraint targets_present check (targets <> ''),
	constraint credential_present check (credential <> ''),
	constraint item_id_matches_kind check ((kind = 'candidate') = (item_id <> '')),
	constraint composed_from_is_a_candidates check (kind = 'candidate' or composed_from = ''),
	constraint torn_down_is_a_candidates check (kind = 'candidate' or torn_down_at = ''),
	constraint torn_down_at_is_time_layout check (torn_down_at = '' or torn_down_at ~ '` + record.TimePattern + `')
)`,

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
