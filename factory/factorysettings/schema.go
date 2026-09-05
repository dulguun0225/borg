package factorysettings

import "github.com/dulguun0225/borg/factory/record"

// Table is the factory-wide settings table this package owns.
const Table = "factory_settings"

// The five tables beside it, each holding the rows of one parameter that has a
// key: one row per key an owner authored a value for.
const (
	// LimitTable holds the attempt limit per stage, plus the interview's rounds
	// and decomposition's re-decompositions, which count against the same
	// parameter.
	LimitTable = "factory_settings_attempt_limit"
	// ReviewSampleRateTable holds the review sample rate per duty.
	ReviewSampleRateTable = "factory_settings_review_sample_rate"
	// RemediationPeriodTable holds the remediation period per advisory severity.
	RemediationPeriodTable = "factory_settings_remediation_period"
	// ReportChannelRateTable holds the report channel's rate per service. The
	// factory-wide rate is a column of [Table], the two being authored the same
	// way and bounding arrival at the way in.
	ReportChannelRateTable = "factory_settings_report_channel_rate"
	// PageCapTable holds the harm mark's page cap per service, with the interval
	// it is counted over.
	PageCapTable = "factory_settings_harm_mark_page_cap"
)

// IDPrefix is what [record.NewID] is called with for the factory-wide settings record.
const IDPrefix = "fs"

// The prefixes of the keyed rows, one per table beside [Table].
const (
	LimitIDPrefix             = "fsl"
	ReviewSampleRateIDPrefix  = "fsr"
	RemediationPeriodIDPrefix = "fsm"
	ReportChannelRateIDPrefix = "fsc"
	PageCapIDPrefix           = "fsp"
)

// FormatVersion is what this package writes into format_version on every
// insert into [Table]. It is at 2 because the record carries every field the
// design puts on it and a row written at 1 carried three.
const FormatVersion = "factory_settings/2"

// The format version each keyed table's rows carry.
const (
	FormatVersionLimit             = "factory_settings_attempt_limit/1"
	FormatVersionReviewSampleRate  = "factory_settings_review_sample_rate/1"
	FormatVersionRemediationPeriod = "factory_settings_remediation_period/1"
	FormatVersionReportChannelRate = "factory_settings_report_channel_rate/1"
	FormatVersionPageCap           = "factory_settings_harm_mark_page_cap/1"
)

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than restated.
//
// only_row is what makes the record a singleton: it is always true and it is
// unique, so a second insert is refused by the store and not by whichever
// caller looked first.
//
// Every authored field is null where an owner authored nothing, the distinction
// every authored parameter carries: an unauthored parameter is the score's to
// supply where the score supplies one and the shipped value's otherwise, and zero
// is a value an owner may mean. allowed_predicate_kinds is the exception the
// design makes for it, one kind per line and empty where an owner extended
// nothing, because its unauthored value is a list.
//
// Two fields are not null and carry what the product ships: harm_mark_pages,
// which is on by default so that an owner who will not be woken by a stranger
// turns it off, and seam_5_enforced, which is off at install and turned on once.
//
// A keyed row exists only where an owner authored one, and each table's unique
// constraint is what re-authoring conflicts on.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	only_row boolean not null default true,
	allowed_predicate_kinds text not null,
	role_prompt_or_skill_threshold double precision,
	advisory_severity double precision,
	held_out_sample_rate double precision,
	decision_log_retention_seconds bigint,
	report_retention_seconds bigint,
	backup_retention_seconds bigint,
	retention_floor_seconds bigint,
	report_channel_rate bigint,
	harm_mark_pages boolean not null default true,
	seam_5_enforced boolean not null default false,
	` + record.Constraints + `,
	constraint only_row_is_true check (only_row),
	constraint one_factory_settings unique (only_row),
	constraint role_prompt_or_skill_threshold_in_range check (
		role_prompt_or_skill_threshold is null
		or (role_prompt_or_skill_threshold >= 0 and role_prompt_or_skill_threshold <= 1)),
	constraint advisory_severity_not_negative check (advisory_severity is null or advisory_severity >= 0),
	constraint held_out_sample_rate_in_range check (
		held_out_sample_rate is null or (held_out_sample_rate >= 0 and held_out_sample_rate <= 1)),
	constraint decision_log_retention_positive check (
		decision_log_retention_seconds is null or decision_log_retention_seconds > 0),
	constraint report_retention_positive check (
		report_retention_seconds is null or report_retention_seconds > 0),
	constraint backup_retention_positive check (
		backup_retention_seconds is null or backup_retention_seconds > 0),
	constraint retention_floor_positive check (
		retention_floor_seconds is null or retention_floor_seconds > 0),
	constraint report_channel_rate_not_negative check (report_channel_rate is null or report_channel_rate >= 0),
	constraint decision_log_retention_above_the_floor check (
		decision_log_retention_seconds is null or retention_floor_seconds is null
		or decision_log_retention_seconds >= retention_floor_seconds)
)`,

	`create table if not exists ` + LimitTable + ` (
	` + record.Columns + `,
	factory_settings_id text not null,
	subject text not null,
	attempt_limit int not null,
	` + record.Constraints + `,
	constraint factory_settings_id_present check (factory_settings_id <> ''),
	constraint subject_known check (subject in ('spec', 'implementation_plan', 'tasks', 'implementation',
		'interview', 'decomposition')),
	constraint attempt_limit_positive check (attempt_limit > 0),
	constraint one_row_per_subject unique (factory_settings_id, subject)
)`,

	`create table if not exists ` + ReviewSampleRateTable + ` (
	` + record.Columns + `,
	factory_settings_id text not null,
	duty int not null,
	rate double precision not null,
	` + record.Constraints + `,
	constraint factory_settings_id_present check (factory_settings_id <> ''),
	constraint duty_in_range check (duty >= 1 and duty <= 12),
	constraint rate_in_range check (rate >= 0 and rate <= 1),
	constraint one_row_per_duty unique (factory_settings_id, duty)
)`,

	`create table if not exists ` + RemediationPeriodTable + ` (
	` + record.Columns + `,
	factory_settings_id text not null,
	severity double precision not null,
	period_seconds bigint not null,
	` + record.Constraints + `,
	constraint factory_settings_id_present check (factory_settings_id <> ''),
	constraint severity_not_negative check (severity >= 0),
	constraint period_positive check (period_seconds > 0),
	constraint one_row_per_severity unique (factory_settings_id, severity)
)`,

	`create table if not exists ` + ReportChannelRateTable + ` (
	` + record.Columns + `,
	factory_settings_id text not null,
	service_id text not null,
	rate bigint not null,
	` + record.Constraints + `,
	constraint factory_settings_id_present check (factory_settings_id <> ''),
	constraint service_id_present check (service_id <> ''),
	constraint rate_not_negative check (rate >= 0),
	constraint one_report_channel_rate_row_per_service unique (factory_settings_id, service_id)
)`,

	`create table if not exists ` + PageCapTable + ` (
	` + record.Columns + `,
	factory_settings_id text not null,
	service_id text not null,
	page_cap int not null,
	interval_seconds bigint not null,
	` + record.Constraints + `,
	constraint factory_settings_id_present check (factory_settings_id <> ''),
	constraint service_id_present check (service_id <> ''),
	constraint page_cap_not_negative check (page_cap >= 0),
	constraint interval_positive check (interval_seconds > 0),
	constraint one_page_cap_row_per_service unique (factory_settings_id, service_id)
)`,
}
