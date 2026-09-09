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
// holds [Decomposition], [NewDecomposition] and [NewID] with
// [Decomposition.Create] and
// [Decomposition.CreateTx], which take a [New], [Decomposition.Supersede] and
// [Decomposition.SupersedeTx], and
// [Decomposition.Repoint] and [Decomposition.RepointTx]. graph.go holds [Hold],
// [RollbackHolds] — the seam [Decomposition.Holds] reads every standing hold
// through at each write — the read of what waits on what, the edges a hold
// imposes, and the cycle check behind [ErrWouldCloseACycle]. dispatch.go holds
// [Dispatch] and [NewDispatch]
// with [Dispatch.Advance], [Dispatch.Enter], [Dispatch.ReturnTo],
// [Dispatch.End], [Dispatch.Escalate],
// [Dispatch.ClearEscalation], [Dispatch.Drop], and [Dispatch.SetPriority].
// read.go holds [Get], [ForIntent], [AtStage], [IDsInArea], [All], [Stages],
// [AllStages], [Live], and [PartlyDelivered]. schema.go holds [Table],
// [StageTable], [IDPrefix], [StageIDPrefix], and [DDL].
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
// [Decomposition.Create] counts the item's first entry to spec,
// [Dispatch.Advance] counts the first entry into each authoring stage after,
// and [Dispatch.Enter] counts a second entry to the stage the item already
// stands at, which is what another attempt at one stage is.
// [Dispatch.ReturnTo] counts nothing — a reject and a rework request send the
// item back to be entered again rather than increment anything themselves —
// and the stages below what it sent the item to re-author counting nothing,
// which is why an advance into a stage the item has already been at counts
// nothing. [Dispatch.ClearEscalation] writes the count the stage stood at when
// a human took the item over, and what the attempt limit is compared against
// is the attempts since that mark.
//
// [Dispatch.Escalate] also records the stage the item stood at, and
// [Dispatch.ClearEscalation] refuses returning the item any later than that
// stage — [ErrEscalationBypass] — so a human taking an escalated item over
// cannot skip the gates between where the factory gave up and where they
// resume it: every item goes through the pipeline and there is no bypass,
// including for one a human is now authoring.
//
// The rework request itself is not written here: it is a row of the decision
// log, appended by the log with whoever was authoring as the actor, and this
// package only moves the item.
//
// What each stage spent is on the agent run record that run wrote and is no
// field of this package: package agentrun owns it, and the query that answers
// what a stage cost is over the run records naming the item and the stage.
//
// The requirements an item answers are ids of package intent's requirement
// record, which intake writes. Every item answers one whole or carries a
// derived share of one, so an item answering none is refused here: work nobody
// asked for has criteria at Spec that trace to nothing. Where those
// requirements are the shares a split derived, each names the item back, so
// the caller mints the item's id with [NewID] and passes it as [New.ID] rather
// than writing the item twice.
//
// The area is the narrowest of the chain of areas whose declarations cover the
// work, chosen here from the chain the caller hands [New.AreaChain] narrowest
// first, and only an area inside the project of the item's service. Walking
// the chain is package area's and a service's project is package service's
// field, and this package imports neither, so the two projects
// [Decomposition.Create] compares are read by the caller and where no declared
// area covers the work there is nothing to compare.
//
// intent_id, service_id, area_id, the ids in waits_on, requirements_answered
// and superseded_by, and item_stage.item_id are id fields and not foreign keys,
// like every link between records; record's doc.go states that rule and its
// cost once. intent_id carries an index, the inbound edge every reading of
// what an intent became follows, declared to the store's schema history as
// "the items of an intent are indexed by the intent".
//
// Two readings walk out of this package. [Live] reads the deploy records of
// the production environment and the release minted for each item, which is
// why this package imports deploy and release and imports nothing else beyond
// record and lease; the environment's targets and which of them a service runs
// on are the caller's to read, and neither edge reaches a writer.
//
// Who may write what: [Decomposition] writes the item at decomposition, the
// pointer at what superseded it, and the repoint, [Dispatch] writes the stage
// wherever the transition comes from — the supersede reports it here like
// every other — the stage an escalation was raised from, beside it, and the
// whole of item_stage, and nothing else writes either table.
//
// What defines it: the fields, dispatch as the writer of the stage and the
// count beside it, the values that end an item, and the two-writer seam are
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/02-what-an-item-names.md
// (C0636, C0637, C0638, C0641, C0646, C0647, C0648, C0650, C0651, C0652, C0653,
// C0654, C0655, C0656, C0662, C0663, C0664, C0669, C0670, C0671, C0672, C0673,
// C0674).
//
// A superseded item pointing at what replaced it in the write that creates
// them, the repointing, the acyclic invariant over what waits on what, the
// rollback hold's edges computed here from [Hold] at every write, refusing a
// write that closes a cycle through a hold and naming the hold, a service that
// does not exist, which requirements an item answers, and a rejected item
// pointing at its replacements, are
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/README.md
// (C0710, C0716, C0750, C0751, C0752, C0753, C0754, C0755, C0757, C0758,
// C0759, C0760, C0763, C0764, C0770, C0785, C0786, C0791, C0792, C0793,
// C0794).
//
// The item owning the record and its links, the stages and what is never a
// field of the item are
// ../../end-goal/how-the-factory-works/01-one-pipeline.md (C0157, C0174,
// C0176, C0210, C0224, C0228, C0231, C0232). The attempt limit the count is
// compared against is
// ../../end-goal/how-the-factory-works/03-gates/05-the-attempt-limit.md
// (C0995, C0996, C0997), and the one way back, with the stages below the
// target re-authoring counting nothing and the item keeping its branch and
// builds until freed, is
// ../../end-goal/how-the-factory-works/03-gates/06-going-back-up.md (C1002,
// C1003, C1004, C1008, C1009, C1022, C1023, C1024, C1025, C1027, C1028,
// C1029, C1035, C1036).
//
// [PartlyDelivered], and reading live off the production deploy record, are
// ../../end-goal/how-the-factory-works/02-intent-into-items/04-when-an-intents-items-do-not-all-ship.md
// (C0797, C0798, C0799, C0800, C0801).
//
// Computing partly delivered from its items, reading live off the production
// deploy record, and re-decomposition superseding only what it replaced, are
// ../../end-goal/how-the-factory-works/02-intent-into-items/02-the-interview.md
// (C0584, C0588, C0597).
//
// The item's branch field and the three endings are
// ../../end-goal/how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md
// (C1424); [Priority], written by [Dispatch.SetPriority] and read by every
// queue, is
// ../../end-goal/how-the-factory-works/05-environments/03-the-merge-queue.md
// (C1551); and every item linking back to the intent that caused it is
// ../../end-goal/how-the-factory-works/07-contracts/10-work-that-spans-services.md
// (C1884).
package item
