package build

import "github.com/dulguun0225/borg/factory/record"

// Table is the build table this package owns.
const Table = "build"

// CoverageTable is the table of what each resolver covered for a build.
const CoverageTable = "build_coverage"

// ResolvedTable is the table of what a build resolved: one row per
// third-party package entry, naming the ecosystem, the source it was
// resolved from, the package, the version, the digest of the content
// resolved, the declared licence, and what required it.
const ResolvedTable = "build_resolved_entry"

// IDPrefix is what [record.NewID] is called with for a build.
const IDPrefix = "bl"

// ResolvedIDPrefix is what [record.NewID] is called with for a resolved
// entry row.
const ResolvedIDPrefix = "ble"

// CoverageIDPrefix is what [record.NewID] is called with for a coverage row.
const CoverageIDPrefix = "blc"

// FormatVersion is what this package writes into format_version on every
// insert into [Table].
const FormatVersion = "build/1"

// FormatVersionResolved is what this package writes into format_version on
// every insert into [ResolvedTable].
const FormatVersionResolved = "build_resolved_entry/1"

// FormatVersionCoverage is what this package writes into format_version on
// every insert into [CoverageTable].
const FormatVersionCoverage = "build_coverage/1"

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than
// restated, so the actor field and its constraints are the same ones every
// record table carries.
//
// item_id and design_system_constraint_id are id fields and not foreign
// keys: the store checks each for being present where it is required and
// never for pointing at anything, and record's doc.go states that rule and
// its cost once. item_id is empty on a search build, which names a service
// and no item; service_id is required on every build, a search build's own
// service among them.
//
// artifact_digest is the digest of the artifact the build runner produced on
// a run that ran. A build stopped before running has no artifact and carries
// its reason in run_reason. resolved_set_could_not_derive is the reason where resolution could not be
// performed at all, empty otherwise, and notice_file is the notice text
// produced from the resolved set in the same write, or the literal "could
// not derive" where the set is — "nothing vulnerable was resolved" and
// "nothing resolved was visible" call for opposite responses, so an absent
// set is a record rather than an empty file.
//
// design_system_constraint_id is empty on a build in a project with no user
// interface. shipped_bundle_identity is on every build and names the release
// of the product that made it, which is what says which way in the build
// carries.
//
// exposure is the exposure list the build runner derived from its own checkout,
// as JSON, and it is the one nullable column here: null is a build no extractor
// ran for, and an empty list is a diff that reached nothing new. The two call
// for opposite responses at a gate — the first resolves the factor and the
// second lowers the number — so they are told apart in the column rather than
// inferred from an empty list. declares_schema_change is the build's own
// reading of whether its checkout ships a schema change, which is what the
// store rule's double application is asked about.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	item_id text not null,
	service_id text not null,
	commit_hash text not null,
	run_state text not null,
	run_reason text not null,
	artifact_digest text not null,
	resolved_set_could_not_derive text not null,
	notice_file text not null,
	design_system_constraint_id text not null,
	shipped_bundle_identity text not null,
	search_origin_build_id text not null,
	search_build boolean not null,
	schema_marks text not null,
	exposure text,
	declares_schema_change boolean not null,
	` + record.Constraints + `,
	constraint service_id_present check (service_id <> ''),
	constraint commit_hash_present check (commit_hash <> ''),
		constraint run_state_known check (run_state in ('started', 'ran', 'did_not_run')),
		constraint artifact_digest_present check ((run_state = 'ran' and artifact_digest <> '') or (run_state in ('started', 'did_not_run') and artifact_digest = '')),
	constraint shipped_bundle_identity_present check (shipped_bundle_identity <> ''),
	constraint search_origin_matches_build check ((search_build = false and search_origin_build_id = '') or (search_build = true and item_id = '' and search_origin_build_id <> ''))
)`,

	`create table if not exists ` + ResolvedTable + ` (
	` + record.Columns + `,
	build_id text not null,
	ecosystem text not null,
	source text not null,
	package text not null,
	version text not null,
	digest text not null,
	licence text not null,
	required_by text not null,
	run_time boolean not null,
	build_time boolean not null,
	` + record.Constraints + `,
	constraint build_id_present check (build_id <> ''),
	constraint ecosystem_present check (ecosystem <> ''),
	constraint package_present check (package <> '')
	)`,

	`create table if not exists ` + CoverageTable + ` (
	` + record.Columns + `,
	build_id text not null,
	ecosystem text not null,
	source text not null,
	base_image_packages boolean not null,
	vendored_source boolean not null,
	statically_linked_code boolean not null,
	digests boolean not null,
		fetch_without_running boolean not null,
		fetch_without_running_reason text not null,
	` + record.Constraints + `,
	constraint build_id_present check (build_id <> ''),
	constraint ecosystem_present check (ecosystem <> ''),
	constraint one_coverage_per_ecosystem unique (build_id, ecosystem)
	)`,
}
