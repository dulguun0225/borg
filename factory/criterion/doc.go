// Package criterion owns a criterion of a service: the three tables its
// record, its withdrawal and its results are written to, the six sentence
// patterns and the classifier that decides which one a sentence is, the
// in-force query, the provenance queries, the encoding derivation, and the
// outcome history a bound reads as unreliable.
//
// # The files
//
// writer.go is [Of], [Draft], [Criterion], [Insert], [Withdraw], [InForce] and
// [Withdrawn]. queries.go is the three provenance queries, [ForConstraint],
// [UnderWithdrawnConstraints] and [ControllingHazard]. schema.go is [Table],
// [WithdrawalTable] and [ResultTable], the three id prefixes and format
// versions, and [DDL]. pattern.go is [Pattern], the six [Patterns] and the
// seventh value, and [Classify]. result.go is [Outcome] with [Outcomes] and
// [Observed], [Place] with [Places], [Outcome.Blocks], [Run], [Result],
// [InsertResults], [RecordResults], [ResultsForBuild], [Latest] and
// [Undecided]. unreliable.go is [Reliability] and [Unreliable]. encoding.go is
// [Encoding], [Derivation], [Derive], [Encodings], [CheckEncodings], and the
// five errors it rejects with.
//
// db_test.go and result_db_test.go are the tests against the database;
// encoding_test.go and pattern_test.go are the two subjects that need none.
//
// # Who writes what
//
// [Insert] and [Withdraw] are the two ways the criterion and withdrawal tables
// are written, and their one caller is the artifact store, which writes them
// in the same call that submits the spec version introducing or withdrawing.
// That is why both take a [pgx.Tx] rather than a pool: they run inside the
// store's spec-submitting transaction, and every record commits or none does.
// What that costs is one of the factory's record-to-record imports, taken
// because the alternative is two writers of one table.
//
// The result table has two writers and the seam between them is the place: the
// build runner writes what the build's own process decided, through
// [InsertResults] inside the transaction that writes the build, and the
// deployer writes what a run on the candidate environment decided, through
// [RecordResults]. Nothing here updates or deletes.
//
// A criterion is written exactly once and never updated, and its id is stable
// and never reused, which [record.NewID] already guarantees. [Classify]
// decides which of the six [Pattern] values a sentence is, and [Insert]
// refuses an unmatched sentence carrying no reason, a matched one carrying a
// reason, and a matched one naming no requirement.
//
// # The in-force query
//
// In force is per build and not per service, and a build is a set of items: the
// ones merged into the repository it was made from, plus the item whose branch
// it is. [InForce] takes that set and reads both halves — introduced by an item
// in the build, and withdrawn by no spec version in it. Which item introduced a
// criterion is a column here and not a hop through the spec version that
// introduced it, so the query neither joins into a table this package does not
// own nor assembles every spec version of every merged item. What that costs is
// the same fact in a second place.
//
// # The encoding derivation
//
// The encoding of a criterion is not a record: it is code in the build, picked
// out by the criterion id it names in a _test.go file, with a marker after the
// id declaring which of the two places decides it. [Derive] is the derivation
// per toolchain — Go is the one with an extractor — and it produces a record
// with a could-not-derive outcome and never an empty list. [CheckEncodings]
// rejects in the gate's directions over what it produced.
//
// # What a run produced
//
// The identity of a result is the build, the run, and the criterion, and each
// run copies the composition it ran against onto its rows. [Latest] is what a
// gate reads, [ResultsForBuild] is every run, and [Undecided] is the
// disagreement between two runs over one build whose compositions match —
// derived at the read, never stored, and read at a gate the way a failure is.
//
// [Unreliable] reads a criterion's outcome history over builds the caller
// chose against a bound the caller read off the service record.
//
// # What is not built here
//
// Four callers this package is written for do not exist yet, and no substitute
// stands in for them: the Spec gate's rejection of a build in an area graded
// irreversible with no criterion in force naming its operation, which is a read
// of [ControllingHazard]; Factory's two constraint listings, which are
// [ForConstraint] and [UnderWithdrawnConstraints]; the intent raised when a
// criterion becomes unreliable, keyed by the criterion; and the service
// record's unreliability bound, which [Unreliable] takes as an argument.
//
// Who may write what: [Insert], [Withdraw], [InsertResults] and
// [RecordResults] insert, and nothing here updates or deletes. The service_id,
// spec_artifact_id, item_id, build_id, criterion_id, requirement_id,
// constraint_derived and hazard_derived columns are id fields and not foreign
// keys; the store checks each for being present where it is required and never
// for pointing at anything, and record's doc.go states that rule and its cost
// once.
//
// What defines it: the criterion, its provenance and its stable id are
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/02-spec/01-the-record.md;
// in force, withdrawal, the outcome history and the unreliable bound are
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/02-spec/02-in-force-and-withdrawal.md;
// the six patterns, the requirement field and the sentence fitting no pattern are
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/02-spec/03-the-six-patterns.md;
// the encoding, the place it declares and the rejection directions are
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/05-implementation/02-the-encoding-and-the-emission.md,
// and what the encoding rests on is
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/05-implementation/03-what-the-encoding-rests-on.md;
// the run, the identity of a result and the composition copied onto it are
// ../../end-goal/how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md;
// the undecided outcome is
// ../../end-goal/how-the-factory-works/05-environments/04-what-the-candidate-environment-decides/01-the-third-outcome.md.
package criterion
