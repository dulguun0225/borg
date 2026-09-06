// Package agentrun owns the agent run record: one per run of any agent,
// written once by the component that performed it, and never rewritten.
//
// agentrun.go holds [Run], [AccountKind] with [AccountKinds], and
// [Run.UnpricedKinds]. schema.go holds [Table], [IDPrefix], and [DDL].
// writer.go holds [Writer], [NewWriter], and [New] with [Writer.Record].
// read.go holds [Get], [ForItem], [ForIntent], [ByAuthorModel], [Spend], and
// [SpendByCredentialSince].
//
// A run carries four groups of fields: what ran — the role, the role prompt
// version in force, the skill versions matched, the model version, and the
// effort; what it ran on — the credential name, the processing location it
// resolved to, the per-person key of whoever lent it, and whether the account
// is a person's own or an organisation's; what it served — an item and its
// stage, or an intent, and the input manifest [package inputmanifest] wrote
// before the run; and what it spent — the units the provider returned per
// kind, the time it returned them, the sources handed over, the rates each
// kind was converted at, and the amount that sums to, absent where a kind
// returned has no rate.
//
// [Writer]'s caller is dispatch, the component that performs a run. What ran
// and what it ran on are written straight onto the record rather than resolved
// through the fleet entry or the People declaration, because the owner may
// change either later without changing what a past record says.
//
// What no run carries yet, each because the record it would be read off is
// not reached: the effort, the processing location and the skill versions,
// which are the fleet entry's, and the fleet entry is not a record here; and
// the lender key, the account kind, the rates and the converted amount, which
// package people holds and dispatch does not read.
//
// What a run served is one of five, and this table takes an item or an intent:
// a stage's run names the item and its stage, and an interview round and a
// decomposition the intent. The grouper run and the evaluation-set run name
// neither, and no run of either is written. The evaluation-set result
// ../../end-goal/records.md inventories with dispatch as its writer, defined
// in ../../end-goal/how-the-factory-works/10-fleet/02-a-model-under-a-name.md
// and keyed by model version, effort, role prompt version, skill versions and
// the set's version, has no package here: the evaluation set is content the
// product ships and nothing ships one, the run is a dispatch onto a fleet
// entry that is not a record, and the design names no shape for a result
// beyond its key.
//
// [Run.UnpricedKinds] is what a spend ceiling names in the hold it writes on
// a credential that fails closed: a run whose converted amount is absent
// because a kind it returned has no rate for that model version and effort.
//
// item_id, intent_id, input_manifest_id, role_prompt_version_id, and the ids
// in skill_version_ids are id fields and not foreign keys, like every link
// between records; record's doc.go states that rule and its cost once.
//
// Who may write what: [Writer] is the one writer, and nothing updates a run
// once recorded.
//
// What defines it: ../../end-goal/how-the-factory-works/10-fleet/01-what-an-agent-runs-on.md
// — the four groups of fields, one record per run, and what ran and what it
// ran on being on the record rather than resolved through the entry or the
// declaration. The sum a spend ceiling compares, the period it derives at the
// read, and a credential failing closed on an unpriced kind are
// ../../end-goal/how-the-factory-works/10-fleet/08-a-spend-ceiling.md.
package agentrun
