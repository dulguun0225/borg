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
// Which caller is not built: dispatch as a component, the fleet entry, and
// the People declaration's per-credential rate. What ran and what it ran on
// are written straight onto the record rather than resolved through those,
// because the owner may change any of them later without changing what a
// past record says.
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
