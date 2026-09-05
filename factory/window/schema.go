package window

import "github.com/dulguun0225/borg/factory/record"

// Table is the analysis window table, and [MarkTable] is the other table this
// package owns: the mark a named human at Ops writes against a rollback.
const Table = "analysis_window"

// MarkTable holds one row per rollback a human marked as not caused by the
// release. It is here rather than in package deploy because the mark is read
// when a window's evidence is read — by everything that learns from outcomes and
// by nothing that acts.
const MarkTable = "rollback_mark"

// IDPrefix is what [record.NewID] is called with for an analysis window, and
// MarkIDPrefix for a mark.
const (
	IDPrefix     = "win"
	MarkIDPrefix = "rbm"
)

// FormatVersion is written into every analysis window record's format_version
// column, and FormatVersionMark into every mark's.
const (
	FormatVersion     = "analysis_window/2"
	FormatVersionMark = "rollback_mark/1"
)

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than restated,
// so the actor field and its constraints are the same ones every record table
// carries.
//
// deploy_id is unique, which is the "one per production deploy" rule in the
// store: a second window over one deploy is refused rather than left for a
// reader to notice two boundaries over one release. release_id is unique where
// it is named, which is the "a release its service has not watched before" rule,
// and it is empty on the window over a deploy the search called for — that
// record names the build and no release, so the uniqueness is a partial index
// rather than a column constraint. build_id is on every window, the build being
// what runs.
//
// exit is empty while the window is open and holds one of the four values when
// it is closed, and closed_at moves with it: exit_and_closed_together enforces
// that in both directions, so a window with an exit and no time and a window
// with a time and no exit are both refused.
//
// The parameters resolved at the open are copied onto the row rather than read
// back from the service record later, and doc.go says why. The per-quantity ones
// are JSON objects keyed by quantity, because the size and the power are one
// value per quantity and a column per quantity would be a column added whenever
// a quantity is; the target set and the operations read alone are one name per
// line, the arrangement deploy's own list column has.
//
// measures_nothing is the window opened over a service missing one of the four
// fields the deployer populates on its service record. It records only that it
// measures nothing, so measures_nothing_records_only_that refuses parameters on
// such a row and refuses a row without them on every other window.
//
// closed_on is the read the window closed on: the four counts per quantity and
// the same per target and operation, as JSON, because what a later reader wants
// of them is per quantity and per series rather than one number.
// closed_on_read_only_when_closed refuses a read on an open window; a skipped
// close carries none, that exit being the one that is not a reading.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	deploy_id text not null unique,
	release_id text not null,
	build_id text not null,
	service_id text not null,
	measures_nothing boolean not null,
	passed_available boolean not null,
	held_out boolean not null,
	sizes text not null,
	powers text not null,
	confidence double precision not null,
	cap_seconds double precision not null,
	boundary_version text not null,
	targets text not null,
	operations_read_alone text not null,
	emission_version_release text not null,
	emission_version_control text not null,
	quantities_outside text not null,
	own_history_sizes text not null,
	own_history_run_length double precision not null,
	threshold_sizes text not null,
	threshold_run_length double precision not null,
	policy_version text not null,
	score_version text not null,
	exit text not null,
	closed_at text not null,
	closed_on text not null,
	finest_size_reached text not null,
	` + record.Constraints + `,
	constraint deploy_id_present check (deploy_id <> ''),
	constraint build_id_present check (build_id <> ''),
	constraint service_id_present check (service_id <> ''),
	constraint boundary_version_present check (boundary_version <> ''),
	constraint policy_version_present check (policy_version <> ''),
	constraint score_version_present check (score_version <> ''),
	constraint confidence_is_a_share check (confidence >= 0 and confidence < 1),
	constraint cap_not_negative check (cap_seconds >= 0),
	constraint own_history_run_length_not_negative check (own_history_run_length >= 0),
	constraint threshold_run_length_not_negative check (threshold_run_length >= 0),
	constraint measures_nothing_records_only_that check (
		(measures_nothing and sizes = '{}' and powers = '{}' and confidence = 0
			and cap_seconds = 0 and not passed_available)
		or (not measures_nothing and sizes <> '{}' and powers <> '{}'
			and confidence > 0 and cap_seconds > 0 and targets <> '')
	),
	constraint exit_known check (exit in ('', 'failed', 'passed', 'timed_out', 'skipped')),
	constraint exit_and_closed_together check ((exit <> '') = (closed_at <> '')),
	constraint closed_on_read_only_when_closed check (exit <> '' or (closed_on = '' and finest_size_reached = '')),
	constraint closed_at_is_time_layout check (closed_at = '' or closed_at ~ '` + record.TimePattern + `')
)`,

	`create unique index if not exists analysis_window_one_per_release
	on ` + Table + ` (release_id) where release_id <> ''`,

	`create table if not exists ` + MarkTable + ` (
	` + record.Columns + `,
	deploy_id text not null unique,
	service_id text not null,
	reason text not null,
	` + record.Constraints + `,
	constraint actor_is_a_human check (actor_kind = 'human'),
	constraint deploy_id_present check (deploy_id <> ''),
	constraint service_id_present check (service_id <> ''),
	constraint reason_present check (reason <> '')
)`,
}
