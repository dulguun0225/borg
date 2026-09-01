// Package criterion owns a criterion of a service: the table its record is
// written to, the six sentence patterns and the classifier that decides which
// one a sentence is, the in-force query, and the encoding checks that tie a
// criterion to the test code that decides it.
//
// # One writer, and it is another package
//
// [Insert] is the one way a criterion is written, and its one caller is the
// artifact store — the record is written in the same call that submits the
// spec version introducing it, so the spec and the criterion cannot disagree
// about what was introduced. That is why [Insert] takes a [pgx.Tx] rather
// than a pool: it runs inside the store's spec-submitting transaction, and
// both records commit or neither does. What that costs is the one
// record-to-record import in the factory — package artifact imports this one
// — taken because the alternative is two writers of one table.
//
// A criterion is written exactly once and never updated. Its id is stable
// and never reused, which [record.NewID] already guarantees: the id is 128
// random bits, so no id is minted twice and none is freed by a withdrawal.
// There is no withdrawal column, because withdrawal is recorded on the
// withdrawing spec version and no spec version withdraws anything in M1.
//
// # The in-force query
//
// In force is per build and not per service, and a build is a set of items: the
// ones merged into the repository it was made from, plus the item whose branch
// it is.
// [InForce] takes that set. A criterion introduced by a sibling item that has not
// merged is a promise this build's tree could not keep, so holding it in force
// against this build would reject every candidate decomposed in parallel with the one
// that introduced it — which is what two candidates at once on one service found.
//
// The other half of the design's query, withdrawal, is not written anywhere yet:
// a spec version withdrawing a criterion arrives with the milestone that authors
// one, so what is filtered so far is introduction alone.
//
// Which item introduced a criterion is a column here and not a hop through the
// spec version that introduced it. The fact is reachable — the artifact names the
// item — and duplicating it is the cheaper of two costs: the alternative is either
// a join into a table this package does not own or a caller that assembles every
// spec version of every merged item to ask about a set of items it already has.
// What it costs is a second place the same fact is written, at one event, by one
// writer.
//
// # The encoding checks
//
// The encoding of a criterion is not a record: it is code in the build,
// picked out by the criterion id it names in a _test.go file. [Encodings]
// reads the ids named under a checkout of the build, and [CheckEncodings]
// rejects in both directions — a criterion in force with no encoding naming
// it, and an encoding naming a criterion not in force — so an item cannot
// leave an encoding in master deciding a promise the service no longer
// makes.
//
// # What the run produced
//
// An encoding runs where there is a run to observe, which is the candidate
// environment, and what it produced attaches to the build. [RecordResults] is
// where the deploy agent writes it when the run finishes, and the identity of a
// result is the build plus the criterion id rather than an id of its own — the
// environment the run happened on is the item's, which the build names.
//
// There are three [Outcome] values and the third is not a kind of pass:
// [OutcomeUndecided] is an encoding that produced a failure and a pass over the
// same build, and [Outcome.Blocks] is what says it is read at the Merge to master gate the
// way a failure is. [Decide] is the whole of how one is reached, which is why the
// encodings are run twice.
//
// The service_id, spec_artifact_id, build_id, and criterion_id columns are id
// fields and not foreign keys, which is the rule for every link between record
// packages. The store checks each for being present and not for pointing at
// anything; record's doc.go states that rule and its cost once.
//
// What defines it: the criterion, its six patterns, the escape, and the
// stable id are ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/02-spec.md; the
// encoding, its authoring rule, and the two rejection directions are
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/05-implementation/README.md; the run, what it
// attaches to, and the undecided outcome are
// ../../end-goal/how-the-factory-works/05-environments/04-what-the-candidate-environment-decides.md.
package criterion
