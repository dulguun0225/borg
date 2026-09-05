package incident

import "github.com/dulguun0225/borg/factory/record"

// Table is the one table this package owns.
const Table = "incident"

// IDPrefix is what [record.NewID] is called with for an incident.
const IDPrefix = "inc"

// FormatVersion is written into every incident record's format_version
// column.
const FormatVersion = "incident/2"

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than restated.
//
// The partial unique index is the deduplication rule in the store: one open
// incident per service and release, so two crossings at once produce one record
// and the second is an observation on it. It is partial because a service that
// has had a resolved incident on a release may have another — the rule is about
// what is open, not about what ever happened.
//
// actor_is_a_component is here as well as in the writer, for the reason package
// policy's owner-only rule is in both: the health monitor is the only writer of this
// record, and a human's judgment about live software reaches production by
// another road.
//
// reading names which of the three readings crossed, quantity which of the
// service's quantities it crossed on, and size beside confidence or run_length
// is the rest of the boundary it was read against: a reading that closes states
// a confidence and one that never does states a run length, and
// reading_states_its_own_boundary is the CHECK that a reading names the pair the
// design says it states and not the other. boundary_version, policy_version and
// score_version are the versions in force at the reading. failure_records is the
// failure records the health monitor copied at the crossing, a field of the
// incident rather than a link to the store.
//
// intent_id is the intent the crossing raised through intake, and is empty on an
// incident that raised none — which is every incident on a release whose window
// is still open, where what follows is a rollback rather than an item.
// observations counts the crossings after the first, and status advances open to
// resolved once and never back.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	environment_id text not null,
	service_id text not null,
	release_id text not null,
	deploy_id text not null,
	reading text not null,
	quantity text not null,
	size double precision not null,
	confidence double precision not null,
	run_length double precision not null,
	boundary_version text not null,
	policy_version text not null,
	score_version text not null,
	failure_records text not null,
	intent_id text not null,
	observations int not null,
	status text not null,
	resolved_at text not null,
	` + record.Constraints + `,
	constraint actor_is_a_component check (actor_kind = 'component'),
	constraint environment_id_present check (environment_id <> ''),
	constraint service_id_present check (service_id <> ''),
	constraint release_id_present check (release_id <> ''),
	constraint deploy_id_present check (deploy_id <> ''),
	constraint reading_known check (reading in ('comparison', 'own_history', 'explicit_threshold')),
	constraint quantity_present check (quantity <> ''),
	constraint size_positive check (size > 0),
	constraint boundary_version_present check (boundary_version <> ''),
	constraint policy_version_present check (policy_version <> ''),
	constraint score_version_present check (score_version <> ''),
	constraint reading_states_its_own_boundary check (
		(confidence > 0) <> (run_length > 0)
		and (reading in ('own_history', 'explicit_threshold')) = (run_length > 0)
	),
	constraint observations_not_negative check (observations >= 0),
	constraint status_known check (status in ('open', 'resolved')),
	constraint resolved_together check ((status = 'resolved') = (resolved_at <> '')),
	constraint resolved_at_is_time_layout check (resolved_at = '' or resolved_at ~ '` + record.TimePattern + `')
)`,

	`create unique index if not exists incident_one_open_per_service_and_release
	on ` + Table + ` (service_id, release_id) where status = 'open'`,
}
