// Package redaction owns the redaction record: what removes one report's
// words, as a second record naming the report, the statement summarizing it,
// or the artifact version quoting it, and the spans removed.
//
// # The files
//
// schema.go is [Table], [IDPrefix], [FormatVersion] and [DDL]. redaction.go
// is [TargetKind] with [TargetKinds] — the three records report text is
// inside — [Target], [Span], [Redaction] as it is stored, [Writing] as an
// owner performs it, [Key], [ErasureAppender], [Writer] and [NewWriter], the
// tx-taking [Insert] package policy calls inside the transaction that appends
// the policy version, and [Reaching], the legal hold's refusal. read.go is
// [OverKind], [ForTarget], [ByErasureKey] and [Get]. db_test.go is the tests,
// every one of them against the database.
//
// # The record never carries the words
//
// A redaction names its target and the bounds of what went, and there is no
// column here a word of the target can be stored in: the spans are byte
// ranges, and the reason is the actor's own account of why the erasure was
// performed. Nothing here deletes a row of any table, and this package writes
// no record but its own — what is erased is text inside a record and never the
// record, so the report row, its grouping link, its count and the measurement
// over it all stand with the words gone.
//
// # Who destroys the bytes
//
// Not this package. Each target's own writer destroys the bytes it holds, on
// a pass of its own, reading [OverKind] for the kind it writes: intake for a
// statement, the artifact store for an artifact version, and the report store
// for a report, which is a second database and is handed its redactions by
// the composition rather than reaching in here. A reader of a target serves
// its words through [ForTarget] from the moment the redaction exists, so a
// store whose destruction lags serves nothing meanwhile.
//
// # The erasure-list row and the key
//
// One erasure writes two things: the erasure-list row on the host, and this
// record. The row lands first — [Insert] calls [ErasureAppender] before it
// writes the record, so a stop between the two leaves a row saying words were
// removed and no record saying they were, which is the event visibly owing
// rather than visibly done, and the row is what a restore replays. So the row
// cannot be keyed by this record's id, which does not exist yet: [Key]
// derives the key from the actor, the target, the reason and the spans, and
// [Insert] hands that same derivation to the appender and writes it into the
// column, so the row and the record cannot be keyed differently. The key is
// unique in [DDL], and [Insert] itself reads by it before appending or
// writing anything: the erasure performed again finds the record the first
// performance wrote and returns it, appending no second row and writing no
// second record, rather than reaching the unique index. [ByErasureKey] is the
// same read for a caller that wants to know first, which package policy's own
// WriteRedaction is, to skip appending a second version too.
//
// [ErasureAppender] is the caller's, because the erasure list has one writer
// and it is the report store, which this package does not import: package
// policy's own WriteRedaction takes one from its caller and hands it to
// [Insert] unchanged, and cmd/factory's own erasure hands it the report
// store's AppendErasure. A call supplying none is refused with
// [ErrNoErasureList] rather than performed without a row.
//
// # Who may write what
//
// [Writer] is Factory, and package policy's own write — WriteRedaction — calls
// [Insert] here and appends the policy version in the same transaction, the
// arrangement a safeguard, a halt and a legal hold already have. A redaction
// is never edited and never withdrawn: the bytes are gone, so there is
// nothing a second record could put back.
//
// A redaction whose target a standing legal hold reaches is refused with
// [ErrLegalHoldReaches] and nothing is written, per
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/03-a-legal-hold.md.
// [Reaching] reads the hold over the whole install itself and takes the
// narrower reading from its caller, the arrangement people.DeleteMapping
// already has, because a hold's subject is a service, a project or the whole
// install and never a target of a redaction. Recording that refusal is not
// built here: it is the caller's, and this package refuses by returning.
//
// What defines it: the record, its writer, what it carries and what it never
// carries, and the erasure-list row beside it are
// ../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/02-reports.md
// (C0440, C0441, C0442, C0445, C0446); the record and its one writer are
// ../../end-goal/records.md (C2793).
//
// A redaction removing text and never a link is
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/02-retention.md
// (C2320).
package redaction
