package item

import "github.com/dulguun0225/borg/factory/record"

// Table is the item table this package owns.
const Table = "item"

// StageTable is the per-stage bookkeeping table this package owns.
const StageTable = "item_stage"

// IDPrefix is what [record.NewID] is called with for an item.
const IDPrefix = "it"

// StageIDPrefix is what [record.NewID] is called with for a per-stage row.
const StageIDPrefix = "its"

// stages is the stage CHECK's value list, written once because both tables
// carry it: a per-stage row cannot name a stage an item cannot be at.
const stages = `('spec', 'implementation', 'queued', 'merged')`

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than restated,
// so the actor field and its constraints are the same ones every record table
// carries.
//
// One row per item and stage is a constraint of the store, which is what
// [Dispatch.ReportAttempt]'s upsert conflicts on.
//
// waits_on holds the ids of the items this one waits on, one per line, and is
// empty where the cut declared none. It is a field and not a table because what
// reads it reads all of one item's at once — the two deploy gates — and a table
// would be a row per edge for a list of two.
//
// priority is signed, so an owner can push an item behind the default as well as
// in front of it, and defaults to nothing at the cut.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	intent_id text not null,
	service_id text not null,
	area_id text not null,
	branch text not null,
	stage text not null,
	waits_on text not null,
	priority bigint not null,
	` + record.Constraints + `,
	constraint intent_id_present check (intent_id <> ''),
	constraint service_id_present check (service_id <> ''),
	constraint branch_present check (branch <> ''),
	constraint stage_known check (stage in ` + stages + `)
)`,

	`create table if not exists ` + StageTable + ` (
	` + record.Columns + `,
	item_id text not null,
	stage text not null,
	attempts int not null,
	spend_tokens bigint not null,
	` + record.Constraints + `,
	constraint item_id_present check (item_id <> ''),
	constraint stage_known check (stage in ` + stages + `),
	constraint attempts_not_negative check (attempts >= 0),
	constraint spend_not_negative check (spend_tokens >= 0),
	constraint one_row_per_item_and_stage unique (item_id, stage)
)`,
}
