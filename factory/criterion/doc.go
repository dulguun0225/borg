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
// The design's query is: a criterion is in force unless a spec version
// withdrawing it belongs to an item in that build. With no withdrawal
// written anywhere yet, the query collapses to every criterion of the
// service, which is what [InForce] returns; the build parameter arrives with
// withdrawal.
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
// The service_id and spec_artifact_id columns are id fields and not foreign
// keys, which is the rule for every link between record packages. The store
// checks each for being present and not for pointing at anything; record's
// doc.go states that rule and its cost once.
//
// What defines it: the criterion, its six patterns, the escape, and the
// stable id are ../../end-goal/how-humans-do-it/03-gates.md#spec; the
// encoding, its authoring rule, and the two rejection directions are
// ../../end-goal/how-humans-do-it/03-gates.md#implementation.
package criterion
