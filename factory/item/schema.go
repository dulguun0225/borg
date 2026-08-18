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

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than restated,
// so the actor field and its constraints are the same ones every record table
// carries.
//
// The two tables carry the same stage CHECK, so a per-stage row cannot name a
// stage an item cannot be at. One row per item and stage is a constraint of
// the store, which is what [Dispatch.ReportAttempt]'s upsert conflicts on.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	intent_id text not null,
	service_id text not null,
	branch text not null,
	stage text not null,
	` + record.Constraints + `,
	constraint intent_id_present check (intent_id <> ''),
	constraint service_id_present check (service_id <> ''),
	constraint branch_present check (branch <> ''),
	constraint stage_known check (stage in ('spec', 'implementation', 'merged'))
)`,

	`create table if not exists ` + StageTable + ` (
	` + record.Columns + `,
	item_id text not null,
	stage text not null,
	attempts int not null,
	spend_tokens bigint not null,
	` + record.Constraints + `,
	constraint item_id_present check (item_id <> ''),
	constraint stage_known check (stage in ('spec', 'implementation', 'merged')),
	constraint attempts_not_negative check (attempts >= 0),
	constraint spend_not_negative check (spend_tokens >= 0),
	constraint one_row_per_item_and_stage unique (item_id, stage)
)`,
}
