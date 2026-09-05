package criterion

import "github.com/dulguun0225/borg/factory/record"

// Table is the criterion table this package owns.
const Table = "criterion"

// ResultTable is the table of what deciding a criterion against a build
// produced, one row per build and criterion.
const ResultTable = "criterion_result"

// IDPrefix is what [record.NewID] is called with for a row of this table.
// The encoding checks read the same prefix: an encoding names a criterion by
// an id of this shape, and [Encodings] finds it by that shape.
const IDPrefix = "cr"

// ResultIDPrefix is what [record.NewID] is called with for a result row. The
// identity of a result is the build and the criterion, so the id is the row's
// and never what anything points at — the prefix differs from [IDPrefix] so that
// [Encodings], which finds a criterion id by its shape, never reads one of these.
const ResultIDPrefix = "crr"

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than
// restated, so the actor field and its constraints are the same ones every
// record table carries.
//
// service_id, spec_artifact_id, and item_id are id fields and not foreign keys,
// each checked for being present and not for pointing at anything; record's
// doc.go states that rule and its cost once. escape_reason is required exactly
// on an escape, because a sentence fitting no pattern is admitted only with a
// tagged reason, and a reason on a matched sentence would let the tag stop
// meaning that.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	service_id text not null,
	spec_artifact_id text not null,
	item_id text not null,
	sentence text not null,
	pattern text not null,
	escape_reason text not null,
	` + record.Constraints + `,
	constraint service_id_present check (service_id <> ''),
	constraint spec_artifact_id_present check (spec_artifact_id <> ''),
	constraint item_id_present check (item_id <> ''),
	constraint sentence_present check (sentence <> ''),
	constraint pattern_known check (pattern in ('always_true', 'event', 'state',
		'unwanted_condition', 'optional_feature', 'state_with_event', 'escape')),
	constraint escape_reason_matches_pattern check ((pattern = 'escape') = (escape_reason <> ''))
)`,

	`create table if not exists ` + ResultTable + ` (
	` + record.Columns + `,
	build_id text not null,
	criterion_id text not null,
	outcome text not null,
	` + record.Constraints + `,
	constraint build_id_present check (build_id <> ''),
	constraint criterion_id_present check (criterion_id <> ''),
	constraint outcome_known check (outcome in ('passed', 'failed', 'undecided')),
	constraint one_row_per_build_and_criterion unique (build_id, criterion_id)
)`,
}
