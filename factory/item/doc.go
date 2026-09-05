// Package item owns the item and its per-stage bookkeeping. The item has two
// writers and the seam is the event: [Decomposition] creates one, points a
// superseded one at what replaced it, and repoints what a standing item waits
// on, and every other write is [Dispatch]'s.
//
// # The code
//
// item.go holds [Item], [StageTotals] with [StageTotals.AttemptsSinceCleared],
// [Stage], the ordered [StageOrder], the [AuthoringStages] an attempt is
// counted at, and the [EveryStage] the CHECK in [DDL] lists. decomposition.go
// holds [Decomposition] and [NewDecomposition] with [Decomposition.Create] and
// [Decomposition.CreateTx], which take a [New], [Decomposition.Supersede], and
// [Decomposition.Repoint] and [Decomposition.RepointTx]. graph.go holds [Edge],
// the read of what waits on what, and the cycle check behind
// [ErrWouldCloseACycle]. dispatch.go holds [Dispatch] and [NewDispatch] with
// [Dispatch.Advance], [Dispatch.ReturnTo], [Dispatch.End], [Dispatch.Escalate],
// [Dispatch.ClearEscalation], [Dispatch.Drop], and [Dispatch.SetPriority].
// read.go holds [Get], [ForIntent], [AtStage], [IDsInArea], [All], [Stages],
// [AllStages], and [PartlyDelivered]. schema.go holds [Table], [StageTable],
// [IDPrefix], [StageIDPrefix], and [DDL].
//
// An item names the intent it was decomposed from, the one service it changes,
// its area, the branch its work is committed on, the items it waits on, which
// of the intent's requirements it answers, and — where a re-decomposition
// replaced it — the items that replaced it. The stage is a field on the item
// and dispatch is what writes it: every stage reports its transition to
// dispatch rather than writing the item itself, so the record's rules are
// implemented once rather than once per stage, and the priority an owner
// reorders a queue with is dispatch's for the same reason.
//
// Queued is the merge queue's membership, the queue having no record of its
// own. Three values end an item and none is in [StageOrder]: dropped,
// escalated, and superseded. A stage value is a CHECK, so each one that
// arrives is a schema edit.
//
// An attempt is counted when a stage is entered to author:
// [Decomposition.Create] counts the item's first entry to spec, and
// [Dispatch.Advance] counts the entry into each authoring stage after.
// [Dispatch.ReturnTo] counts nothing — a reject and a rework request send the
// item back to be entered again rather than increment anything themselves.
// [Dispatch.ClearEscalation] writes the count the stage stood at when a human
// took the item over, and what the attempt limit is compared against is the
// attempts since that mark.
//
// The rework request itself is not written here: it is a row of the decision
// log, appended by the log with whoever was authoring as the actor, and this
// package only moves the item.
//
// What each stage spent is on the agent run record that run wrote and is no
// field of this package: package agentrun owns it, and the query that answers
// what a stage cost is over the run records naming the item and the stage.
//
// Which caller is not built: dispatch as a component. Nothing here re-enters a
// stage an item was returned to, so the second attempt at a stage is counted by
// the component that puts an agent on it, which does not exist —
// [Dispatch.Advance] and [Decomposition.Create] are the two entries that are
// built. The requirements an item answers are ids of a record package intent
// does not write yet, so the field is empty on every item today. The projects
// [Decomposition.Create] compares are read by the caller for the same reason:
// a project is not a record yet, and where both are empty the comparison
// passes.
//
// intent_id, service_id, area_id, the ids in waits_on, requirements_answered
// and superseded_by, and item_stage.item_id are id fields and not foreign keys,
// like every link between records; record's doc.go states that rule and its
// cost once.
//
// Who may write what: [Decomposition] writes the item at decomposition, the
// supersede, and the repoint, [Dispatch] writes every other field and the whole
// of item_stage, and nothing else writes either table.
//
// What defines it: the fields, dispatch as the writer of the stage and the
// count beside it, the values that end an item, and the two-writer seam are
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/02-what-an-item-names.md.
// A superseded item pointing at what replaced it, the repointing, and the
// acyclic invariant over what waits on what are
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/README.md.
// The stages and what is never a field of the item are
// ../../end-goal/how-the-factory-works/01-one-pipeline.md. The attempt limit
// the count is compared against is
// ../../end-goal/how-the-factory-works/03-gates/05-the-attempt-limit.md, and
// the one way back is
// ../../end-goal/how-the-factory-works/03-gates/06-going-back-up.md.
// [PartlyDelivered] is
// ../../end-goal/how-the-factory-works/02-intent-into-items/04-when-an-intents-items-do-not-all-ship.md.
package item
