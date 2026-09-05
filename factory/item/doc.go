// Package item owns the item and its per-stage bookkeeping. The item has two
// writers and the seam is the event: [Decomposition] writes an item at
// decomposition and writes one again only to point a superseded item at what
// replaced it, and every other write is [Dispatch]'s.
//
// # The code
//
// item.go holds [Item], [StageTotals], [Stage], the ordered [StageOrder] and
// the [EveryStage] the CHECK in [DDL] lists. decomposition.go holds
// [Decomposition] and [NewDecomposition] with [Decomposition.Create], which
// takes a [New], and [Decomposition.Supersede], refusing a second supersede
// with [ErrAlreadySuperseded] and a merged item with [ErrMerged]. dispatch.go
// holds [Dispatch] and [NewDispatch] with [Dispatch.Advance],
// [Dispatch.ReworkRequest], [Dispatch.ReportAttempt], and
// [Dispatch.SetPriority]. read.go holds [Get], [ForIntent], [AtStage],
// [IDsInArea], [All], [Stages], and [AllStages]. schema.go holds [Table],
// [StageTable], [IDPrefix], [StageIDPrefix], and [DDL].
//
// An item names the intent it was decomposed from, the one service it changes,
// the branch its work is committed on, the items it waits on, and — where a
// re-decomposition replaced it — the items that replaced it. The stage is a
// field on the item and dispatch is what writes it: every stage reports its
// transition to dispatch rather than writing the item itself, so the record's
// rules are implemented once rather than once per stage, and the priority an
// owner reorders a queue with is dispatch's for the same reason.
//
// Queued is the merge queue's membership, the queue having no record of its
// own. Superseded is terminal: it is not in [StageOrder], so nothing advances
// to it and nothing is sent back to it. A stage value is a CHECK, so each one
// that arrives is a schema edit. An item moves both ways: [Dispatch.Advance]
// goes one stage forward and [Dispatch.ReworkRequest] to the stage the item is
// at or one above it, counting one attempt at what it is sent to.
//
// [Dispatch.ReportAttempt] keeps one row per item and stage: how many attempts
// the stage has taken and what it spent, in tokens, kept per stage rather than
// reset at each advance. The row's actor and time are the first report's; a
// later report adds to the totals, validates its actor, and stores it nowhere,
// so which attempt spent what is not in the record.
//
// intent_id, service_id, the ids in waits_on and superseded_by, and
// item_stage.item_id are id fields and not foreign keys, like every link
// between records; record's doc.go states that rule and its cost once.
//
// Who may write what: [Decomposition] writes the item at decomposition and the
// supersede, [Dispatch] writes every field after and the whole of item_stage,
// and nothing else writes either table.
//
// What defines it: the fields, dispatch as the writer of the stage and the two
// counts beside it, and the two-writer seam are
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/02-what-an-item-names.md.
// A superseded item pointing at what replaced it is
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/README.md.
package item
