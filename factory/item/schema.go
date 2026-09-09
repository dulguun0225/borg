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

// FormatVersion is what this package writes into format_version on every
// insert into [Table].
const FormatVersion = "item/1"

// FormatVersionStage is what this package writes into format_version on every
// insert into [StageTable].
const FormatVersionStage = "item_stage/1"

// stages is the stage CHECK's value list, written once because both tables
// carry it: a per-stage row cannot name a stage an item cannot be at.
const stages = `('spec', 'implementation_plan', 'tasks', 'implementation', 'queued', 'merged',
		'dropped', 'escalated', 'superseded')`

// escalatedFromStages is escalated_from_stage's CHECK value list: the empty
// string an item carries before it has ever escalated, and the four
// [AuthoringStages] Escalate may write it from. It is written with "= any
// (array[...])" rather than "in (...)", the way [stages] is: the column name
// ends in "stage" and TestDDLListsEveryStage finds every "stage in (" in [DDL]
// verbatim, so an "in (" clause here would read as a third table carrying the
// nine-stage CHECK.
const escalatedFromStages = `'', 'spec', 'implementation_plan', 'tasks', 'implementation'`

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than restated,
// so the actor field and its constraints are the same ones every record table
// carries.
//
// One row per item and stage is a constraint of the store, which is what the
// entry count's upsert conflicts on.
//
// item_by_intent is the inbound edge from the intent, which every reading of
// what an intent became follows: whether its items all shipped is asked of
// every intent a screen lists, so the reading is only as cheap as that edge is
// indexed. It costs a write per item on a table decomposition appends to and
// nothing else inserts into.
//
// superseded_by holds the ids of the items that replaced this one, one per line,
// and is empty on every item nothing replaced. It is a field for the reason
// waits_on is: what reads it reads one item's at a time, and a table would be a
// row per edge for a list of two.
//
// waits_on holds the ids of the items this one waits on, one per line, and is
// empty where decomposition declared none. It is a field and not a table because what
// reads it reads all of one item's at once — the two deploy gates — and a table
// would be a row per edge for a list of two.
//
// requirements_answered holds the ids of the intent's requirements this item
// answers, one per line, and is a field for the same reason: what reads it
// reads one item's at a time.
//
// priority is signed, so an owner can push an item behind the default as well as
// in front of it, and defaults to nothing at decomposition.
//
// escalated_from_stage is the stage [Dispatch.Escalate] wrote when it wrote
// escalated, empty on an item that has never escalated. [Dispatch.ClearEscalation]
// reads it to refuse a target later than that stage.
//
// cleared_at_attempts is the count an escalation was cleared at, zero where
// none ever was, and never above attempts: what the attempt limit compares
// against is the difference.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	intent_id text not null,
	service_id text not null,
	area_id text not null,
	branch text not null,
	stage text not null,
	escalated_from_stage text not null default '',
	waits_on text not null,
	requirements_answered text not null,
	superseded_by text not null,
	priority bigint not null,
	` + record.Constraints + `,
	constraint intent_id_present check (intent_id <> ''),
	constraint service_id_present check (service_id <> ''),
	constraint branch_present check (branch <> ''),
	constraint stage_known check (stage in ` + stages + `),
	constraint escalated_from_stage_known check (escalated_from_stage = any (array[` + escalatedFromStages + `]))
)`,

	// A store this package already applied before escalated_from_stage existed
	// has the table without the column; this is the same statement a fresh
	// create already carries, added rather than assumed, so both paths agree.
	`alter table ` + Table + ` add column if not exists escalated_from_stage text not null default ''`,

	`create table if not exists ` + StageTable + ` (
	` + record.Columns + `,
	item_id text not null,
	stage text not null,
	attempts int not null,
	cleared_at_attempts int not null,
	` + record.Constraints + `,
	constraint item_id_present check (item_id <> ''),
	constraint stage_known check (stage in ` + stages + `),
	constraint attempts_not_negative check (attempts >= 0),
	constraint cleared_at_attempts_within_attempts check (cleared_at_attempts >= 0 and cleared_at_attempts <= attempts),
	constraint one_row_per_item_and_stage unique (item_id, stage)
)`,

	`create index if not exists item_by_intent on ` + Table + ` (intent_id)`,
}
