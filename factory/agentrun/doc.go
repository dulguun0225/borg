// Package agentrun owns the agent run record: one per run of any agent,
// written once by the component that performed it, and never rewritten.
//
// agentrun.go holds [Run], [AccountKind] with [AccountKinds], and
// [Run.UnpricedKinds]. schema.go holds [Table], [IDPrefix], and [DDL].
// writer.go holds [Writer], [NewWriter], and [New] with [Writer.Record].
// read.go holds [Get], [ForItem], [ForIntent], [ByAuthorModel], [Spend],
// [Period] with [PeriodUnit], [PeriodUnits] and [DateLayout], and
// [SpendByCredentialIn].
//
// A run carries four groups of fields: what ran — the role, the role prompt
// version in force, the skill versions matched, the model version, and the
// effort; what it ran on — the credential name, the processing location it
// resolved to, the per-person key of whoever lent it, and whether the account
// is a person's own or an organisation's; what it served — an item and its
// stage, an intent, or the project a role put on one served, and the input
// manifest [package inputmanifest] wrote before the run; and what it spent — the units the provider returned per
// kind, the time it returned them, the sources handed over, the rates each
// kind was converted at, and the amount that sums to, absent where a kind
// returned has no rate.
//
// The amount is computed by [Writer.Record] from the units and the rates the
// record stores, and an amount its caller supplies that is not that sum is
// refused: what the record says a run cost is its own two fields multiplied
// out. The time the provider returned the units is the caller's to supply and
// its absence is refused, because the time the record was written is a
// different fact and the sum a ceiling compares is over the first.
//
// The tests depart from the shape a record package takes: db_test.go holds the
// record's own, and spend_test.go the sum a spend ceiling compares, split by
// subject because the two together pass the 500-line bound.
//
// [Writer]'s caller is dispatch, the component that performs a run. What ran
// and what it ran on are written straight onto the record rather than resolved
// through the fleet entry or the People declaration, because the owner may
// change either later without changing what a past record says.
//
// What every run carries is what dispatch reads at the run: the model version,
// the effort and the processing location off the fleet entry it matched, and
// the lender's per-person key, the account kind and the rates per kind off the
// People declaration naming the credential. The processing location, the
// lender's key and the account kind are required: a run missing one is a run
// whose account or processing location no later reading can name, and a run
// from before the fields existed is not a case this store holds. The
// skill versions are the one field of the four groups still empty: a skill is a
// record nothing writes.
//
// What a run served is one of five, and this table takes an item, an intent or
// a project: a stage's run names the item and its stage, an interview round and
// a decomposition the intent, and the grouper the project it was put on, which
// is its whole subject — it runs before there is an intent and reads that
// project's reports and nothing else. The evaluation-set run names none of the
// three, and no run of one is written. The evaluation-set result
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
// [SpendByCredentialIn] is the sum that ceiling compares, over one credential
// and one currency, bounded at both ends of the period that contains the
// instant it is asked for. Which period a run falls in is on no record: the
// read derives it from the [Period] the owner authored — a start date, the zone
// it was authored in, and a length — so a period lengthened or re-anchored
// later re-buckets every run already written. That vocabulary is package
// people's, repeated here rather than imported, the two packages being the
// spend ceiling's two halves.
//
// item_id, intent_id, project_id, input_manifest_id, role_prompt_version_id,
// and the ids in skill_version_ids are id fields and not foreign keys, like every link
// between records; record's doc.go states that rule and its cost once.
//
// Who may write what: [Writer] is the one writer, and nothing updates a run
// once recorded.
//
// What defines it:
// ../../end-goal/how-the-factory-works/10-fleet/01-what-an-agent-runs-on.md
// (C2362, C2367, C2380, C2381, C2382, C2383, C2384, C2386, C2388, C2389, C2390,
// C2391) — the four groups of fields, one record per run, and what ran and what
// it ran on being on the record rather than resolved through the entry or the
// declaration. The sum a spend ceiling compares, the period it derives at the
// read, and a credential failing closed on an unpriced kind are
// ../../end-goal/how-the-factory-works/10-fleet/08-a-spend-ceiling.md (C2519,
// C2524, C2525, C2526, C2529, C2533, C2534, C2543). Whose account paid for a
// run, recorded here and on the fleet entry and never on the artifact, is
// ../../end-goal/how-the-factory-works/10-fleet/04-paid-for-is-not-authored-by.md
// (C2452).
//
// The run put on a project, the named credential every run of report-derived
// work spends through, and the processing location every run names, the
// grouper's own reads of arriving reports included, are
// ../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/02-reports.md
// (C0379, C0461, C0462).
//
// Also ../../end-goal/records.md (C2834).
package agentrun
