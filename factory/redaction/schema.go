package redaction

import "github.com/dulguun0225/borg/factory/record"

// Table is the redaction record's table.
const Table = "redaction"

// IDPrefix is what [record.NewID] is called with for a redaction.
const IDPrefix = "rdn"

// FormatVersion is written into every redaction record's format_version
// column.
const FormatVersion = "redaction/1"

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than
// restated, so the actor is the same field every record table carries and
// the time the row was written is the at column every one of them has.
//
// There is no column here a word of the target can be stored in: the record
// names what was removed by its bounds and never by its content, and the
// reason is the actor's own words about why. spans is one or more half-open
// byte ranges in the encoding [encodeSpans] writes, and a redaction naming
// none destroys nothing, so the CHECK refuses an empty one.
//
// erasure_key is the key the erasure-list row of this erasure was appended
// under, which [Key] derives from what the erasure is over. It is unique
// because it is what makes the second step of the event keyed: an erasure
// performed again derives the key of the row already appended and finds the
// record already written.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	target_kind text not null,
	target_id text not null,
	spans text not null,
	reason text not null,
	erasure_key text not null,
	` + record.Constraints + `,
	constraint target_kind_known check (target_kind in ('report', 'statement', 'artifact_version')),
	constraint target_id_present check (target_id <> ''),
	constraint spans_present check (spans <> ''),
	constraint reason_present check (reason <> ''),
	constraint erasure_key_present check (erasure_key <> '')
)`,

	`create index if not exists redaction_by_target on ` + Table + ` (target_kind, target_id)`,

	`create unique index if not exists redaction_by_erasure_key on ` + Table + ` (erasure_key)`,
}
