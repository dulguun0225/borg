// Package criterion owns a criterion of a service: the table its record is
// written to, the six sentence patterns and the classifier that decides which
// one a sentence is, the in-force query, and the encoding checks that tie a
// criterion to the test code that decides it.
//
// # The files
//
// writer.go is [Criterion], [Insert], and [InForce]. schema.go is [Table] and
// [ResultTable], [IDPrefix] and [ResultIDPrefix], and [DDL]. pattern.go is
// [Pattern], the six [Patterns], and [Classify]. result.go is [Outcome], the
// three [Outcomes], [Outcome.Blocks], [Decide], [Result], [RecordResults], and
// [ResultsForBuild]. encoding.go is [Encodings], [CheckEncodings], and the two
// errors it rejects with, [NotEncodedError] and [NotInForceError].
//
// db_test.go is the tests against the database; encoding_test.go,
// pattern_test.go, and result_test.go are the three subjects that need none.
//
// [Insert] is the one way a criterion is written, and its one caller is the
// artifact store, which writes it in the same call that submits the spec
// version introducing it. That is why [Insert] takes a [pgx.Tx] rather than a
// pool: it runs inside the store's spec-submitting transaction, and both
// records commit or neither does. What that costs is one of the factory's two
// record-to-record imports, taken because the alternative is two writers of one
// table.
//
// A criterion is written exactly once and never updated, and its id is stable
// and never reused, which [record.NewID] already guarantees.
//
// [Classify] decides which of the six [Pattern] values a sentence is, and
// [Insert] refuses an unmatched sentence carrying no escape reason and a matched
// one carrying a reason.
//
// # The in-force query
//
// In force is per build and not per service, and a build is a set of items: the
// ones merged into the repository it was made from, plus the item whose branch
// it is. [InForce] takes that set. Which item introduced a criterion is a column
// here and not a hop through the spec version that introduced it, so the query
// neither joins into a table this package does not own nor assembles every spec
// version of every merged item. What that costs is the same fact in a second
// place.
//
// # The encoding checks
//
// The encoding of a criterion is not a record: it is code in the build, picked
// out by the criterion id it names in a _test.go file. [Encodings] reads the ids
// named under a checkout of the build, and [CheckEncodings] rejects in both
// directions — a criterion in force with no encoding naming it, and an encoding
// naming a criterion not in force.
//
// # What the run produced
//
// [RecordResults] is where the deploy agent writes what the encodings produced,
// keyed by the build plus the criterion id rather than an id of its own;
// [ResultsForBuild] reads them back.
//
// There are three [Outcome] values and the third is not a kind of pass:
// [OutcomeUndecided] is an encoding that produced a failure and a pass over the
// same build, and [Outcome.Blocks] is true of it as it is of a failure. [Decide]
// is the whole of how one is reached, which is why the encodings are run twice.
//
// Who may write what: [Insert] and [RecordResults] insert, and nothing here
// updates or deletes. The service_id, spec_artifact_id, build_id, and
// criterion_id columns are id fields and not foreign keys; the store checks each
// for being present and not for pointing at anything, and record's doc.go states
// that rule and its cost once.
//
// What defines it: the criterion, its six patterns, the escape, and the
// stable id are ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/02-spec/README.md; the
// encoding, its authoring rule, and the two rejection directions are
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/05-implementation/README.md; the run, what it
// attaches to, and the undecided outcome are
// ../../end-goal/how-the-factory-works/05-environments/04-what-the-candidate-environment-decides/README.md.
package criterion
