// Package inputmanifest owns the input manifest: what context assembly hands
// an agent before one run, named by reference and never by value, written
// once per dispatch before the agent starts.
//
// inputmanifest.go holds [Manifest], [Material], and [Exclusion].
// schema.go holds [Table], [IDPrefix], and [DDL]. writer.go holds [Writer],
// [NewWriter], and [New] with [Writer.Write]. read.go holds [Get].
//
// A manifest names the item and its stage, or the intent, the dispatch was
// for; the material handed over, one entry per source with its class,
// reference, and size; the read-at-once bound applied; the selection rule
// version in force; and what was excluded before the agent ran, each with the
// reason — the selection rule, or a class of material the fleet entry the run
// dispatched on does not name.
//
// Which caller is not built: context assembly. The design has it write the
// manifest at every dispatch, ahead of the run; nothing in this wave performs
// a dispatch, so the caller that would be context assembly — the component
// that dispatches an agent — holds [Writer] and calls [Writer.Write] itself
// until context assembly exists. read_at_once_bound is nullable and
// selection_rule_version may be empty for the matching reason: the fleet
// entry and the selection rule are records later work adds, so a manifest
// written today carries neither.
//
// item_id and intent_id are id fields and not foreign keys, like every link
// between records; record's doc.go states that rule and its cost once.
//
// Who may write what: [Writer] is the one writer, and nothing updates a
// manifest once written — a later change to the fleet entry or the selection
// rule does not reach back into a manifest a past run was handed.
//
// What defines it: ../../end-goal/how-the-factory-works/01-one-pipeline.md,
// under "What did not fit is recorded" — context assembly, the manifest's
// fields, naming by reference, and the exclusion and its reason.
package inputmanifest
