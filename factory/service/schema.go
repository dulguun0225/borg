package service

import "github.com/dulguun0225/borg/factory/record"

// Table is the service table this package owns.
const Table = "service"

// WindowSizeTable and WindowPowerTable are the two per-quantity tables: one row
// per service and quantity an owner authored a value for. The window's size and
// its power are one value per quantity, because a detectable change in an error
// rate and one in a latency quantile are not one number, and every other
// authored value on a service is one number and a column of [Table].
const (
	WindowSizeTable  = "service_window_size"
	WindowPowerTable = "service_window_power"
)

// ExplicitThresholdTable is the third per-quantity table: the absolute number a
// safeguard set for one service and one quantity, and the size it is read at.
// It is per quantity for the reason the size and the power are, and it is a
// table rather than two columns because a service may carry a threshold on one
// quantity and none on another.
const ExplicitThresholdTable = "service_explicit_threshold"

// SeedTable and ValueSetTable are the two version tables: what the candidate's
// store starts with, and the non-production values the configuration takes on a
// candidate. Each authoring is a version written beside the earlier ones and
// never editing one, so the version a run was composed from stays readable.
const (
	SeedTable     = "service_seed_version"
	ValueSetTable = "service_value_set_version"
)

// IDPrefix is what [record.NewID] is called with for a service, and the four
// beside it are for the rows of the four tables above.
const (
	IDPrefix                  = "svc"
	WindowSizeIDPrefix        = "sws"
	WindowPowerIDPrefix       = "swp"
	ExplicitThresholdIDPrefix = "set"
	SeedIDPrefix              = "ssv"
	ValueSetIDPrefix          = "svs"
)

// FormatVersion is what this package writes into format_version on every insert
// into [Table], and the four beside it on every insert into the others.
const (
	FormatVersion                  = "service/3"
	FormatVersionWindowSize        = "service_window_size/1"
	FormatVersionWindowPower       = "service_window_power/1"
	FormatVersionExplicitThreshold = "service_explicit_threshold/1"
	FormatVersionSeed              = "service_seed_version/1"
	FormatVersionValueSet          = "service_value_set_version/1"
)

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than restated,
// so the actor field and its constraints are the same ones every record table
// carries.
//
// The name is unique in the store, so two decompositions creating one service
// name is a refused insert and not two services. name, repository and project_id
// are the identity decomposition writes and no later write moves any of them.
//
// operation_cap is how many operations one release may hold open per interval,
// the cap the failure record's keys already take, and overflow_operation is the
// name the excess lands in, so a service that names an operation per identifier
// grows the store by the cap and no further. The two are authored together and
// are absent together, which operation_cap_names_its_overflow enforces.
//
// environment_hour_rate and instance_hour_rate price the hosting a candidate and
// a kept fleet consume outside the factory: converted at the write, they turn the
// recorded environment-hours and instance-hours into money. Where none is
// authored those figures stay units only.
//
// search_budget_builds and search_budget_seconds are what a search may spend,
// each build it deploys putting a build that passed no gate in front of real
// traffic. Where an owner authors neither, the budget is what bisecting a batch
// of the backlog cap's size needs, which package healthmonitor supplies.
//
// Every number an owner authors is null where they authored nothing, which is
// not the same as a value of zero: for a gate-policy parameter an unauthored
// value is the score's to supply, and for one authored outright — the objective,
// the paging hours, the two retentions — nothing supplies it and absence is the
// design's own default, stated in the file doc.go names for each.
//
// The bake volume, the backlog cap and the instance-hour rate sit beside the
// window limit rather than among gate policy's eleven rows: the first two are
// supplied where an owner authors none — the score supplies the bake volume, and
// the backlog cap is the window limit — and the rate is authored outright, an
// absent one being a deploy record whose amount and rate are null together and
// not an amount of zero. A rate of zero is a real value and is not absence.
//
// The four booleans are the deployer's and false until it writes them, which is
// what deployer_wrote_at tells apart from a deployer that wrote false: absent
// where nothing has adopted the service.
//
// The repository credential is two names on a host that can tell master from a
// branch and one where it cannot, which is what
// repository_credential_shape_matches_names enforces in both directions: shape
// `one` with a master credential named, and shape `two` without one, are both
// refused by the store and not only by the writer.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	name text not null unique,
	repository text not null,
	project_id text not null,
	provisioned_at text not null,
	repository_credential_shape text not null,
	repository_credential_branch text not null,
	repository_credential_master text not null,
	retired_at text not null,
	targets text not null,
	window_confidence double precision,
	window_cap_seconds double precision,
	window_limit double precision,
	exposure_bound double precision,
	bake_volume double precision,
	backlog_cap double precision,
	instance_hour_rate double precision,
	environment_hour_rate double precision,
	operation_cap double precision,
	overflow_operation text not null default '',
	search_budget_builds double precision,
	search_budget_seconds double precision,
	mutant_cap double precision,
	failure_record_key_cap double precision,
	unreliable_bound double precision,
	incident_item_bound_seconds double precision,
	snapshot_retention_seconds double precision,
	objective double precision,
	objective_period_seconds double precision,
	paging_hours_start text not null,
	paging_hours_end text not null,
	paging_hours_zone text not null,
	product_licence text not null,
	target_reached boolean not null,
	instances_replaceable boolean not null,
	rollback_path_present boolean not null,
	emission_readable boolean not null,
	deployer_wrote_at text not null,
	` + record.Constraints + `,
	constraint name_present check (name <> ''),
	constraint repository_present check (repository <> ''),
	constraint project_id_present check (project_id <> ''),
	constraint provisioned_at_is_time_layout check (provisioned_at = '' or provisioned_at ~ '` + record.TimePattern + `'),
	constraint retired_at_is_time_layout check (retired_at = '' or retired_at ~ '` + record.TimePattern + `'),
	constraint deployer_wrote_at_is_time_layout check (deployer_wrote_at = '' or deployer_wrote_at ~ '` + record.TimePattern + `'),
	constraint repository_credential_shape_known check (repository_credential_shape in ('', 'one', 'two')),
	constraint repository_credential_shape_matches_names check (
		(repository_credential_shape = '' and repository_credential_branch = '' and repository_credential_master = '')
		or (repository_credential_shape = 'one' and repository_credential_branch <> '' and repository_credential_master = '')
		or (repository_credential_shape = 'two' and repository_credential_branch <> '' and repository_credential_master <> '')
	),
	constraint provisioned_names_its_credentials check ((provisioned_at <> '') = (repository_credential_shape <> '')),
	constraint window_confidence_is_a_share check (window_confidence is null or (window_confidence > 0 and window_confidence <= 1)),
	constraint window_cap_positive check (window_cap_seconds is null or window_cap_seconds > 0),
	constraint window_limit_positive check (window_limit is null or window_limit > 0),
	constraint exposure_bound_positive check (exposure_bound is null or exposure_bound > 0),
	constraint bake_volume_positive check (bake_volume is null or bake_volume > 0),
	constraint backlog_cap_positive check (backlog_cap is null or backlog_cap > 0),
	constraint instance_hour_rate_not_negative check (instance_hour_rate is null or instance_hour_rate >= 0),
	constraint environment_hour_rate_not_negative check (environment_hour_rate is null or environment_hour_rate >= 0),
	constraint operation_cap_positive check (operation_cap is null or operation_cap > 0),
	constraint operation_cap_names_its_overflow check ((operation_cap is null) = (overflow_operation = '')),
	constraint search_budget_builds_positive check (search_budget_builds is null or search_budget_builds > 0),
	constraint search_budget_seconds_positive check (search_budget_seconds is null or search_budget_seconds > 0),
	constraint mutant_cap_positive check (mutant_cap is null or mutant_cap > 0),
	constraint failure_record_key_cap_positive check (failure_record_key_cap is null or failure_record_key_cap > 0),
	constraint unreliable_bound_is_a_share check (unreliable_bound is null or (unreliable_bound >= 0 and unreliable_bound <= 1)),
	constraint incident_item_bound_positive check (incident_item_bound_seconds is null or incident_item_bound_seconds > 0),
	constraint snapshot_retention_positive check (snapshot_retention_seconds is null or snapshot_retention_seconds > 0),
	constraint objective_is_a_share check (objective is null or (objective > 0 and objective <= 1)),
	constraint objective_names_its_period check ((objective is null) = (objective_period_seconds is null)),
	constraint objective_period_positive check (objective_period_seconds is null or objective_period_seconds > 0),
	constraint paging_hours_are_whole check (
		(paging_hours_start = '' and paging_hours_end = '' and paging_hours_zone = '')
		or (paging_hours_start <> '' and paging_hours_end <> '' and paging_hours_zone <> '')
	)
)`,

	`create table if not exists ` + WindowSizeTable + ` (
	` + record.Columns + `,
	service_id text not null,
	quantity text not null,
	size double precision not null,
	` + record.Constraints + `,
	constraint service_id_present check (service_id <> ''),
	constraint quantity_present check (quantity <> ''),
	constraint size_is_a_share check (size > 0 and size <= 1),
	constraint one_size_per_service_and_quantity unique (service_id, quantity)
)`,

	`create table if not exists ` + WindowPowerTable + ` (
	` + record.Columns + `,
	service_id text not null,
	quantity text not null,
	power double precision not null,
	` + record.Constraints + `,
	constraint service_id_present check (service_id <> ''),
	constraint quantity_present check (quantity <> ''),
	constraint power_is_a_share check (power > 0 and power < 1),
	constraint one_power_per_service_and_quantity unique (service_id, quantity)
)`,

	`create table if not exists ` + ExplicitThresholdTable + ` (
	` + record.Columns + `,
	service_id text not null,
	quantity text not null,
	threshold double precision not null,
	size double precision not null,
	` + record.Constraints + `,
	constraint service_id_present check (service_id <> ''),
	constraint quantity_present check (quantity <> ''),
	constraint threshold_is_a_share check (threshold >= 0 and threshold <= 1),
	constraint threshold_size_is_a_share check (size > 0 and size <= 1),
	constraint one_threshold_per_service_and_quantity unique (service_id, quantity)
)`,

	`create table if not exists ` + SeedTable + ` (
	` + record.Columns + `,
	service_id text not null,
	digest text not null,
	content text not null,
	` + record.Constraints + `,
	constraint service_id_present check (service_id <> ''),
	constraint digest_present check (digest <> '')
)`,

	`create table if not exists ` + ValueSetTable + ` (
	` + record.Columns + `,
	service_id text not null,
	digest text not null,
	content text not null,
	` + record.Constraints + `,
	constraint service_id_present check (service_id <> ''),
	constraint digest_present check (digest <> '')
)`,
}
