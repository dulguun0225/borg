package people

import "github.com/dulguun0225/borg/factory/record"

// Table is the one table this package owns.
const Table = "people_declaration"

// IDPrefix is what [record.NewID] is called with for a declaration.
const IDPrefix = "ppl"

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than restated.
//
// one_holding is the rule that a row names a duty or an obligation and never
// both, written as a CHECK because it is the whole shape of the record: duty is
// zero exactly where obligation is set. The duty range is one to twelve, which is
// the twelve of what-humans-do.md and is why nothing here may write a thirteenth.
//
// The unique constraint is over all three columns rather than over the human and
// one of them, because the pair a declaration is about is the human plus whichever
// holding it names — and it is what [Writer.Declare]'s insert conflicts on, so
// declaring the same holding twice is one row.
//
// actor_is_a_human is here as well as in the writer, the mirror of the incident
// record's actor_is_a_component: distributing the twelve is the owner's, and a
// component doing it would be the factory deciding who holds the factory's
// obligations.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	human text not null,
	duty int not null,
	obligation text not null,
	withdrawn_at text not null,
	` + record.Constraints + `,
	constraint actor_is_a_human check (actor_kind = 'human'),
	constraint human_present check (human <> ''),
	constraint one_holding check (
		(duty between 1 and 12 and obligation = '')
		or (duty = 0 and obligation in ('hosting', 'driftdetector', 'fleet'))
	),
	constraint one_row_per_human_and_holding unique (human, duty, obligation),
	constraint withdrawn_at_is_time_layout check (withdrawn_at = '' or withdrawn_at ~ '` + record.TimePattern + `')
)`,
}
