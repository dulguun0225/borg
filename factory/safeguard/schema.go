package safeguard

import "github.com/dulguun0225/borg/factory/record"

// Table is the safeguard record's table.
const Table = "safeguard"

// WithdrawalTable holds a safeguard's withdrawal: a second record naming the
// safeguard it ends, written pending and marked approved by a second write —
// the gate row that decides it, when one exists. The safeguard is in force
// until an approved withdrawal names it.
const WithdrawalTable = "safeguard_withdrawal"

// IDPrefix is what [record.NewID] is called with for a safeguard.
const IDPrefix = "sfg"

// WithdrawalIDPrefix is what [record.NewID] is called with for a withdrawal.
const WithdrawalIDPrefix = "sfgw"

// FormatVersion is written into every safeguard record's format_version
// column. It is at 2 because the record now carries the subject key and the
// routing field, and no longer carries withdrawn.
const FormatVersion = "safeguard/2"

// FormatVersionWithdrawal is written into every withdrawal record's
// format_version column.
const FormatVersionWithdrawal = "safeguard_withdrawal/1"

// DDL is this package's schema. [record.Columns] and [record.Constraints] are
// composed rather than restated.
//
// The bound is four columns because a parameter's value is a number, a list, or
// one predicate, and never two of the three: bound is null unless the parameter's
// kind is numeric, bound_list is empty unless it is a list, the two predicate
// columns are empty unless it is a predicate, and a safeguard that adds a human
// carries none of them. The CHECK refuses more than one at once, which is as far
// as the store can go — which shape a parameter takes is package gatepolicy's
// table, and restating it here would be one fact in two places able to disagree.
//
// subject_key narrows the subject to one value of a parameter's own key, where
// the parameter has one: the gate row for the risk threshold, the stage for the
// attempt limit, the duty for the review sample rate, the severity for the
// remediation period, the quantity for the window's size and power, and the
// service for the report channel's per-service rate and the harm mark's page
// cap. It is empty for a parameter with no key of its own.
//
// route_duty and route_human_key are the routing field: the duty or the named
// human a safeguard's rows route to, where it puts a human at a gate. At most
// one is set; where neither is, the rows go where every unheld row goes, to the
// owner.
//
// Withdrawing a safeguard is a second record, [WithdrawalTable], and not a
// field flipped in place: a field flipped in place would leave nothing saying
// the check ever existed, when it was removed, or by whom.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	parameter text not null,
	subject_kind text not null,
	subject_id text not null,
	subject_key text not null default '',
	direction text not null,
	bound double precision,
	bound_list text not null,
	predicate_kind text not null,
	predicate_argument text not null,
	route_duty int,
	route_human_key text not null default '',
	` + record.Constraints + `,
	constraint parameter_present check (parameter <> ''),
	constraint subject_kind_known check (subject_kind in ('stage', 'service', 'project', 'area',
		'contract_element', 'design_system_component', 'factory_settings', 'report_store',
		'drift_detector_last_check')),
	constraint subject_id_present check (subject_id <> ''),
	constraint direction_known check (direction in ('ceiling', 'floor', 'adds_a_human')),
	constraint one_bound_at_most check (
		(case when bound is null then 0 else 1 end)
		+ (case when bound_list = '' then 0 else 1 end)
		+ (case when predicate_kind = '' then 0 else 1 end) <= 1),
	constraint predicate_argument_needs_a_kind check (predicate_kind <> '' or predicate_argument = ''),
	constraint route_duty_in_range check (route_duty is null or (route_duty >= 1 and route_duty <= 12)),
	constraint route_names_at_most_one check (route_duty is null or route_human_key = '')
)`,

	`create index if not exists safeguard_by_subject on ` + Table + ` (subject_kind, subject_id, subject_key)`,

	`create table if not exists ` + WithdrawalTable + ` (
	` + record.Columns + `,
	safeguard_id text not null,
	approved boolean not null default false,
	approved_at text,
	` + record.Constraints + `,
	constraint safeguard_id_present check (safeguard_id <> ''),
	constraint approved_at_matches_approval check (
		(approved and approved_at is not null) or (not approved and approved_at is null)),
	constraint approved_at_is_time_layout check (approved_at is null or approved_at ~ '` + record.TimePattern + `')
)`,

	`create index if not exists safeguard_withdrawal_by_safeguard on ` + WithdrawalTable + ` (safeguard_id)`,
}
