package build

import "github.com/dulguun0225/borg/factory/record"

// Table is the build table this package owns.
const Table = "build"

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

// FormatVersion is what this package writes into format_version on every
// insert into [Table].
const FormatVersion = "build/1"

// FormatVersionResolved is what this package writes into format_version on
// every insert into [ResolvedTable].
const FormatVersionResolved = "build_resolved_entry/1"

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
// artifact_digest is the digest of the artifact the build runner produced,
// required on every build. resolved_set_coverage is JSON, a map from
// ecosystem to what the resolver read there — a map and not a column per
// ecosystem, because the set of ecosystems a build touches is not fixed.
// resolved_set_could_not_derive is the reason where resolution could not be
// performed at all, empty otherwise, and notice_file is the notice text
// produced from the resolved set in the same write, or the literal "could
// not derive" where the set is — "nothing vulnerable was resolved" and
// "nothing resolved was visible" call for opposite responses, so an absent
// set is a record rather than an empty file.
//
// design_system_constraint_id is empty on a build in a project with no user
// interface. shipped_bundle_identity is empty except on a search build,
// which names the release of the product that made it.
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
	artifact_digest text not null,
	resolved_set_coverage text not null,
	resolved_set_could_not_derive text not null,
	notice_file text not null,
	design_system_constraint_id text not null,
	shipped_bundle_identity text not null,
	exposure text,
	declares_schema_change boolean not null,
	` + record.Constraints + `,
	constraint service_id_present check (service_id <> ''),
	constraint commit_hash_present check (commit_hash <> ''),
	constraint artifact_digest_present check (artifact_digest <> ''),
	constraint one_build_per_commit unique (item_id, service_id, commit_hash)
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
	` + record.Constraints + `,
	constraint build_id_present check (build_id <> ''),
	constraint ecosystem_present check (ecosystem <> ''),
	constraint package_present check (package <> '')
)`,
}
