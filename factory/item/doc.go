// Package item owns the item and its per-stage bookkeeping. The item has two
// writers and the seam is the event: [Cut] writes an item once, at the cut,
// and never writes an item again — the type has no update method — and every
// later write is [Dispatch]'s.
//
// # The item
//
// An item names the intent it was cut from, the one service it changes, and
// the branch its work is committed on. Which stage it is at is a field on the
// item, and dispatch is what writes it: every stage reports its transition to
// dispatch rather than writing the item itself, so the record's rules are
// implemented once rather than once per stage.
//
// The stage CHECK in [DDL] lists spec, implementation, and merged — the
// stages M1's path touches. The design has more (superseded, dropped,
// escalated), and later milestones widen the CHECK as they build what writes
// each. What that costs: a CHECK is a schema edit each time a value arrives.
//
// # The per-stage row
//
// [Dispatch.ReportAttempt] keeps one row per item and stage: how many
// attempts the stage has taken and what it spent, in tokens. The totals are
// kept per stage rather than reset at each advance, because the rework rate
// reads them later. The row's actor and time are the first report's; a later
// report adds to the totals, validates its actor, and stores it nowhere —
// which attempt spent what is not in the record.
//
// # Links
//
// intent_id and service_id are id fields and not foreign keys, like every
// link between records, and item_stage.item_id is written the same way. The
// store checks a link for being present and not for pointing at anything; the
// link walk reads the fields. What that costs: an item naming an intent or a
// service that does not exist is stored without complaint, and the cut is
// trusted to name records it just wrote or read. record's doc.go states the
// present rule and its cost once, for every link column in the graph.
//
// What defines it:
// ../../end-goal/how-humans-do-it/02-intent-into-items.md#what-an-item-names,
// which sets the fields, dispatch as the writer of the stage and the two
// counts beside it, and the two-writer seam.
package item
