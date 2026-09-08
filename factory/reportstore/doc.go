// Package reportstore owns the report store: the store an end user's report
// is written into, sized for what a population of end users sends rather than
// for what a graph joins on, and the only thing that may remove one. It is
// also the one writer of the erasure list kept beside it.
//
// # The files
//
// schema.go is [ReportTable] and [CounterTable], [ReportIDPrefix],
// [FormatVersionReport], [Shape] with [Shapes], [Kind] with [Kinds], [DDL],
// and this store's own [URLEnv], [URL], [Open] and [Apply]. reportstore.go is
// [Report] as it is stored, [Submission], [Refusal] and [Result], [Span] and
// [Redaction], the four kinds an erasure reaches, [ErasureKindReport] and its
// neighbours, the five interfaces the composition implements — [Deploys],
// [Settings], [Holds], [ReadEvents] and [Redactions] — and [Store] with
// [NewStore]. submit.go is arrival: [Store.Submit], [RatePeriod],
// [HarmMarkedShare] and [SourceShare], and the counters a refusal and an
// unreadable submission are counted on. read.go is [Store.Get], [Count] with
// [Store.Counts], and the two reads the pass that groups reports makes —
// [Store.UngroupedIn], which says whether it has anything to group, and
// [Store.Reports], which answers with the words — and [Store.AwaitingAdmission],
// the reports Work renders while the safeguard holding one stands. grouping.go
// is [Store.Link], [Store.Admit], [Store.ByIntent], [Group] with
// [Store.Grouped] and [Store.Ungrouped]. retention.go is
// [Retired] and [Store.Retire]. redact.go is [Store.AppendErasure],
// [Store.Redact], [Store.RedactionPass] and [Store.Replay].
//
// The tests are db_test.go, submit_test.go, retention_test.go and
// redact_test.go, every one of them against the database.
//
// This package brings its own [Open], [Apply], [URL] and [DDL] rather than
// reaching for package postgres, the arrangement driftdetector already has:
// the factory's schema applier knows every record package and is called at
// the start of every factory process, and a store the factory applies is a
// store the factory owns. [URL] has no default of its own, where
// driftdetector's does: this store is on the path every submission takes, and
// a store nobody named would be a channel silently writing into the factory's
// own database, so a missing URL is an error its caller reports.
//
// # A departure from the record shape
//
// Every other record package's table composes [record.Columns] and
// [record.Constraints], so every row carries the actor that wrote it. There
// are no actor columns here and the row names no person at all. The channel
// carries no identity by design: the opaque key on the row is not a reporter
// handle, nothing joins it to a person, and it cannot be added to a report
// already written, so an actor column would be the one field able to make the
// channel identify who submitted. What this package takes from package record
// is the identifier and the timestamp format, so an instant here reads back
// the way one in the graph does.
//
// # Arrival
//
// [Store.Submit] is the whole of it. The way in presents the token the
// deployer minted at the deploy that placed it, and the deploy record that
// token's digest finds is what says which service and which environment the
// report is against — never the submission. A submission naming no deploy
// this store knows is refused and counted on the whole channel and never on a
// service, so a safeguard narrowing one service's rate is not evaded by
// submitting under another service's name.
//
// The two rates an owner authored are read through [Settings] and enforced
// here: the factory-wide one over the whole channel, the per-service one over
// one service, either at zero closing what it bounds. [HarmMarkedShare] of
// each is reserved for a report marking harm and [SourceShare] is what one
// opaque key may spend, both fixed rather than authored. [RatePeriod] is what
// a rate is counted over, which the design leaves to the store.
//
// The counters are the two numbers Factory reads that no query can produce:
// the refusals, because the record a query would count is the write the rate
// exists to refuse, and the submissions written under a shape this store does
// not read, because a submission that could not be read is the loss the
// refused counter cannot see. Both are lost if the counter is lost, where
// [Store.Ungrouped] and every other number are derived from the reports.
//
// # Erasure
//
// A redaction is a record of the factory's graph, and this store is a second
// database, so the composition hands the redactions across through
// [Redactions] rather than their writer reaching in here. [Store.Redact] reads
// the spans against the report's own words, appends the erasure-list row —
// keyed by the erasure key the action computed and handed across, so a step
// taken again writes nothing — and then destroys the named spans in place.
// That order is the event's: the row, the destruction, and the redaction
// record last, so a stop leaves it visibly owing rather than visibly done, and
// a span outside the words fails before any row lands.
// [Store.RedactionPass] is this store's own pass over every redaction naming
// a report, and [Store.Replay] destroys again what the list says was removed,
// which is what a restore is served through before this store serves anything.
// [Store.Get] reads a report through the redactions naming it whether or not
// the pass has run, so a store whose destruction lags serves nothing meanwhile,
// and appends a read event through [ReadEvents] before it answers with words.
//
// # Who may write what
//
// [Store] is the one writer of every row here and, through
// [Store.AppendErasure], the one writer of the erasure list in the factory:
// nothing else calls that list's own append. Its two callers are the ones the
// design gives it — Factory at a redaction, for whichever of the three records
// the redaction names, and People at a mapping deletion. Nothing else may remove a report:
// [Store.Retire] is retention's own pass, refused for a service a legal hold
// reaches and removing nothing at all where an owner authored no retention.
// Grouping removes nothing — [Store.Link] marks the report with the intent it
// was grouped into and keeps it, which is what the rate of reports before and
// after a release rests on.
//
// [Store.Reports] answers with the grouped reports beside the ungrouped ones,
// because deciding which reports are one problem is a decision over all of
// them, and it admits only the reports a human has admitted where its caller
// says the admission safeguard is in force. Which of the two that is is not
// read here: a safeguard is a record of the factory's graph and this store is
// a second database.
//
// What is not built here: the way in that presents a submission, the entrance
// it reaches, the notice a report names, and the safeguard that makes a report
// wait for a human's admission are each their own package or their own step,
// and this store is reached through [Store.Submit] and the reads above by all
// of them.
//
// What defines it: the report, the store, arrival, the counters, retention
// and the erasure are
// ../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/02-reports.md
// (C0375, C0376, C0383, C0386, C0387, C0388, C0389, C0390, C0391, C0415,
// C0416, C0417, C0418, C0420, C0421, C0422, C0423, C0424, C0425, C0426,
// C0427, C0428, C0430, C0431, C0435, C0447, C0448, C0449, C0453, C0470,
// C0471, C0472); the component and the record it writes are
// ../../end-goal/components.md (C0004) and ../../end-goal/records.md (C2792).
package reportstore
