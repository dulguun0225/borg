// Package build owns the build record: one per commit built, naming the
// service it was built for, the item where it has one, the commit, the
// artifact digest when it ran, a run state and reason when it did not, the shipped-bundle identity of the release of the product
// that made it, the resolved set of third-party packages, the notice file
// derived from that set, the design system constraint in force where the
// project has screens, the exposure list the build runner derived from its own
// checkout, whether that checkout declares a schema change, and what the
// build's own process decided about the criteria whose encodings declare it.
// The record is written before the process runs and completed on that same
// record after it returns.
//
// The record exists to be pointed at — the encoding of each criterion runs
// against a checkout of it, the gate's open event names it, and the release
// made from it points back at it. What happened to the build beyond what it
// produced is written where it happened, by the component it happened at.
// A rebuild of the same commit is another record. Search builds name the
// release build they came from, including its design-system constraint.
//
// writer.go is [ResolvedEntry], [Coverage], [Draft], [Build], [Writer] and [NewWriter]
// with [Writer.Create], the one write, and the reads [Get], [ForCommit],
// [ForServiceCommit], [Newest], [Resolved] and [Exposure]; coverage.go reads
// resolver coverage; schema.go is [Table], [ResolvedTable], [CoverageTable],
// the three id prefixes, the three format versions, and [DDL]. The tests are db_test.go, every one of them
// against the database.
//
// # Item and service
//
// item_id is empty on a search build, which names a service and no item —
// the search's builds are of commits on no branch and decided by no gate.
// service_id is required on every build, item-bound or not. A rebuild of the
// same commit is another row, so the record holds every attempt.
// item_id and design_system_constraint_id are id fields and not foreign
// keys — a cross-package link is a field the link walk reads, and the store
// does not check either for pointing at anything.
//
// # The resolved set and its coverage
//
// [Draft.Resolved] is written to [ResolvedTable], one row per package: the
// ecosystem, the source it was resolved from, the package, the version, the
// digest of the content, the declared licence, and what required it. An entry
// whose resolver could not produce a digest, a licence, or the requiring edge
// carries that field empty, which is that entry's own coverage and not an
// error to fill in later. [Draft.Coverage] is typed evidence of resolver
// coverage, including whether fetching can be separated from running, and
// [Resolved] reads the package table back, which is what the merge queue compares the
// re-resolved set's digests against. [Draft.ResolvedSetCouldNotDerive] is the
// reason where resolution could not be performed at all — a record and not an empty set, because "nothing
// vulnerable was resolved" and "nothing resolved was visible" call for
// opposite responses. [Draft.NoticeFile] is produced from the same set in the
// same write.
//
// # What the build read of its own checkout
//
// [Draft.Exposure] is the exposure list package exposure derived from the diff
// between the base and this build's commit, and nil where no extractor ran for
// the toolchain; [Exposure] is the read of it, a read of its own rather than a
// field of [Build], and it answers false for a build the record holds none for.
// [Draft.DeclaresSchemaChange] is whether that checkout ships a schema change,
// which is what enforcement's store rule asks before it requires the candidate
// environment to have applied the change twice; [Build] carries it as a field.
//
// # What the build's own process decided
//
// [Draft.Results] is recorded through [criterion.InsertResults] inside the
// same transaction as the build row, as run 0 with [criterion.PlaceBuild] —
// the run and place [criterion.RecordResults] uses for a run on the candidate
// environment do not apply here, because this is the build's own process and
// not a later deploy, so [Writer.Create] calls [criterion.InsertResults]
// directly rather than wrapping it in a second transaction.
//
// # Callers
//
// Four: the implementation stage when it finishes, the candidate deploy where
// a rebuild is needed, the merge queue at re-verification when it builds the
// candidate branch onto the master it will actually merge into, and the search
// whose builds are of commits on no branch and name no item. The first caller
// is the command-line interface, which calls [Writer.Create] and then
// [Writer.Complete].
//
// Who may write what: [Writer.Create] inserts into [Table] and
// [ResolvedTable], and [Writer.Complete] updates only the run result on the
// started row. A rebuild is another record, while completing a build is not.
//
// What defines it: the build record, the resolved set, its coverage, the notice
// file, and the design system constraint field in
// ../../end-goal/how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md
// (C1433, C1435, C1437, C1439, C1440, C1441, C1443, C1444, C1448, C1449,
// C1450, C1459, C1460, C1464, C1466, C1467, C1469, C1478);
//
// what a build is called and the search's builds are
// ../../end-goal/how-the-factory-works/06-releases/03-what-a-build-is-called-and-when.md
// (C1647, C1650); the notice file and could-not-derive as they reach the
// release are
// ../../end-goal/how-the-factory-works/06-releases/02-the-release-record.md
// (C1643, C1644);
//
// the exposure list read from a diff and a build record in
// ../../end-goal/how-the-factory-works/04-risk-score/01-factors-at-least.md;
// the schema change a build declares and the double application the candidate
// environment performs on it in
// ../../end-goal/how-the-factory-works/07-contracts/09-the-store-is-a-contract-too.md;
// and where the criterion's run happens, in
// ../../end-goal/how-the-factory-works/05-environments/04-what-the-candidate-environment-decides/README.md
// (C1596).
package build
