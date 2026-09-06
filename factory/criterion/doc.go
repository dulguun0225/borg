// Package criterion owns a criterion of a service: the four tables its record,
// its withdrawal, its results and the mutation read over its encodings are
// written to, the six sentence patterns and the classifier that decides which
// one a sentence is, the in-force query, the provenance queries, the encoding
// derivation, the mutation score's own derivation, and the outcome history a
// bound reads as unreliable.
//
// # The files
//
// writer.go is [Of], [Draft], [Criterion], [Insert], [Withdraw], [InForce] and
// [Withdrawn]. queries.go is [Provenance] with [Provenances], the five
// provenance queries — [HumanConfirmed], [WithdrawalsWithAnAuthority] with
// [WithdrawalWithAnAuthority], [ForConstraint], [UnderWithdrawnConstraints] and
// [ControllingHazard] — and the rejection made from the last,
// [CheckHazardControlled] with [HazardUncontrolledError].
// mutation.go is [Mutation] with [Mutation.Score], [Mutation.Derived] and
// [Mutation.Blocks], and [DeriveMutation] with [MutationTools].
// mutationwrite.go is [MutationReading], [RecordMutation], [LatestMutation]
// and [MutationsForBuild]. schema.go is [Table], [WithdrawalTable],
// [ResultTable] and [MutationTable], the four id prefixes and format versions,
// and [DDL]. pattern.go is [Pattern], the six [Patterns] and the
// seventh value, and [Classify]. result.go is [Outcome] with [Outcomes] and
// [Observed], [Place] with [Places], [Outcome.Blocks], [Run], [Result],
// [InsertResults], [RecordResults], [ResultsForBuild], [Latest] and
// [Undecided]. unreliable.go is [Reliability] and [Unreliable]. encoding.go is
// [Encoding], [Derivation], [Derive], [Encodings], [CheckEncodings], and the
// five errors it rejects with.
//
// db_test.go, result_db_test.go, hazard_db_test.go, provenance_db_test.go and
// mutation_db_test.go are the tests against the database; encoding_test.go,
// pattern_test.go and mutation_test.go are the three subjects that need none.
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
// chose against a bound the caller read off the service record, authored or
// not: [service.UnreliableBoundInForce] is what resolves it, reading the
// shipped default where an owner authored none rather than the field's zero
// value, which would mark a criterion unreliable at its first disagreement.
//
// # The mutation score
//
// Whether an encoding could have failed is a reading on the build and no
// factor of the score: [DeriveMutation] mutates a checkout and produces the
// share of the seeded defects the encodings caught, with a coverage field and a
// could-not-derive outcome, and [RecordMutation] writes it beside that run's
// criteria results. The score itself is derived from the two counts at the
// read, the way undecided is. [Mutation.Blocks] is what the Merge to master
// gate asks: a score below the mutation floor rejects there on the terms an
// undecided criterion does, and a build the factory could not mutate never
// passes.
//
// The derivation is per toolchain and Go is the one with an extractor. It runs
// where the checkout is, reads the coverage of the checkout's own test run, and
// mutates only where the checkout names one of [MutationTools] in a tool
// directive of go.mod. The mutant cap authored on the service record is not
// read here: it bounds what the deployer deploys, and the deployer's mutation
// pass is the caller this is written for.
//
// # What is not built here
//
// Three callers this package is written for do not exist yet, and no
// substitute stands in for them: Factory's two constraint listings, which are
// [ForConstraint] and [UnderWithdrawnConstraints]; and whatever reports what a
// service promises that a human confirmed, which is [HumanConfirmed]. The
// deployer's mutation pass at the candidate run is not one of them:
// [DeriveMutation] and [RecordMutation] are what it calls, and the
// command-line interface composes it.
//
// [Unreliable] itself now resolves the bound through
// [service.UnreliableBoundInForce], the field being service.SetUnreliableBound's
// to write and policy.Factory.AuthorUnreliableBound's to author, and the
// command-line interface calls it where the candidate run's criterion results
// are recorded, writing what it read onto the gate's own criterion result so
// Merge to master reads the same reading the run took. Becoming unreliable
// raises the intent this package's own doc names, keyed by the criterion
// through [intent.Evidence.CriterionID], with the command-line interface's
// own dedup over [intent.OnEvidence] standing in for the design's "a second
// raise while that intent is open joins it" — the two narrowings the design
// puts on the outcome history, one seed version and a diff reaching the
// requirement, are not derived anywhere yet, so the caller reads every build
// of the candidate rather than that filtered set.
//
// [WithdrawalsWithAnAuthority] is read by the score, through the reader the
// command-line interface composes for it: each withdrawal is a resolved factor
// at the Spec row, routed to the human that provenance names. It takes the spec
// versions a human decided as an argument, the decision being the decision log's
// fact and not this table's, and that caller walks the log for them.
//
// Who may write what: [Insert], [Withdraw], [InsertResults],
// [RecordResults] and [RecordMutation] insert, and nothing here updates or
// deletes. The service_id,
// spec_artifact_id, item_id, build_id, criterion_id, requirement_id,
// constraint_derived and hazard_derived columns are id fields and not foreign
// keys; the store checks each for being present where it is required and never
// for pointing at anything, and record's doc.go states that rule and its cost
// once.
//
// What defines it: the criterion, its provenance and its stable id are
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/02-spec/01-the-record.md;
// in force, withdrawal, the withdrawal of a criterion whose provenance names an
// authority, the outcome history and the unreliable bound are
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
// ../../end-goal/how-the-factory-works/05-environments/04-what-the-candidate-environment-decides/01-the-third-outcome.md;
// the mutation score, its coverage and its could-not-derive outcome are
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/05-implementation/03-what-the-encoding-rests-on.md,
// and the mutation floor it is read against is
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/07-merge-to-master.md;
// the hazard-derived criterion and the Spec gate's mechanical rejection are
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/03-hazard-severity.md.
package criterion
