package criterion

import "github.com/dulguun0225/borg/factory/record"

// Table is the one table this package owns.
const Table = "criterion"

// IDPrefix is what [record.NewID] is called with for a row of this table.
// The encoding checks read the same prefix: an encoding names a criterion by
// an id of this shape, and [Encodings] finds it by that shape.
const IDPrefix = "cr"

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than
// restated, so the actor field and its constraints are the same ones every
// record table carries.
//
// service_id and spec_artifact_id are id fields and not foreign keys, each
// checked for being present and not for pointing at anything — doc.go says
// what that costs. escape_reason is required exactly on an escape, because a
// sentence fitting no pattern is admitted only with a tagged reason, and a
// reason on a matched sentence would let the tag stop meaning that.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	service_id text not null,
	spec_artifact_id text not null,
	sentence text not null,
	pattern text not null,
	escape_reason text not null,
	` + record.Constraints + `,
	constraint service_id_present check (service_id <> ''),
	constraint spec_artifact_id_present check (spec_artifact_id <> ''),
	constraint sentence_present check (sentence <> ''),
	constraint pattern_known check (pattern in ('always_true', 'event', 'state',
		'unwanted_condition', 'optional_feature', 'state_with_event', 'escape')),
	constraint escape_reason_matches_pattern check ((pattern = 'escape') = (escape_reason <> ''))
)`,
}
