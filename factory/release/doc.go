// Package release owns the release record and the number: one row per release,
// written at one of two events and never written again.
//
// # The files
//
// writer.go is [Release], [Minting], [Writer] and [NewWriter] with
// [Writer.Mint] and [Writer.MintWith], and [Get]. count.go is the other reads —
// [All], [Highest], [ForItem], [Above], [Below], [Between] — with the counts a
// factor is read from, [CountForItemsSince], [CountForService] and
// [ItemsWithRelease]. authorship.go is [AuthorshipRollup] and the [Stage] it
// answers with. schema.go is [Table], [IDPrefix], [AdvisoryLockKey] and [DDL].
//
// db_test.go is the tests against the database, and lock_test.go is the one
// subject that needs none: the advisory lock key recomputed from the name it
// is derived from.
//
// The record is where the graph joins. It names the build it is made of and the
// commit that build was made from, and the item that caused it where one did;
// every deploy of it names the release, and the gate decisions name the item and
// the build and never the release.
//
// # The two occasions, and the one writer
//
// The merge queue writes it, and mints the release number with it. The
// fast-forward that merges a candidate is the first occasion, and the record
// names the item. A commit that reached master by another path is the second:
// the queue holds the service until a human accepts the commit, then builds it,
// re-verifies it as it re-verifies a candidate, and mints from the same writer a
// release naming a build and no item — [Release.NamesAnItem] is what a reader
// tells the two apart by. A release naming no item is one no gate decided: no
// consumer contract is derived for it, no prior moves, its [AuthorshipRollup] is
// empty, and every traversal from it ends at the acceptance rather than at an
// intent.
//
// Nothing here names a contract version. A contract version names the release
// and copies its number, so a release names what it publishes as an inbound
// edge, and a column would be the same fact twice on a record that has no
// update. [Writer.MintWith] is how the versions are written instead: the merge
// queue writes them inside this transaction, so a number and the versions its
// release publishes commit together or not at all.
//
// The authorship rollup is a query for the same reason: which of the three
// authorship attributes wrote each stage is the artifact store's fact, and a
// column here would be a second copy of it.
//
// # The number
//
// The number is an ordinal, per service, minted with the record in one
// transaction: [Writer.MintWith] takes a per-service advisory lock, reads the
// highest number the service has, and inserts one above the higher of that and
// [Minting.Floor], so two mints for one service serialise and the numbers have
// no gap and no duplicate. The floor is what the first mint after a restore of
// the factory's records needs: the second reading it is taken from is a store
// outside those records, and the queue is what reads it. The
// unique constraint on (service_id, number) is the same rule in the store,
// refusing what a skipped lock would produce. Numbers are never reused: a
// rolled-back release keeps its number, and the fix that follows takes the
// next one. The order they come out in is the order the caller mints in, which
// for the queue is master's order.
//
// The record is keyed on the commit, so writing one commit again writes nothing
// and returns the release already written: the fast-forward and this write are
// one operation restartable from either side. What master holds is not read
// here — the queue reads master itself, at every start and before every mint,
// and [Highest] is the service's highest-numbered release and nothing about a
// branch.
//
// Who may write what: [Writer.Mint] and [Writer.MintWith] insert into release
// and update and delete nothing. Written once is a property of the API — there
// is no update method — and of the store, which refuses a second release over
// one commit and a second over one item.
//
// What defines it: the release record in
// ../../end-goal/how-the-factory-works/06-releases/02-the-release-record.md — its
// two write occasions, its single writer, and the authorship rollup as a query
// — one item per release in
// ../../end-goal/how-the-factory-works/06-releases/01-one-item-per-release.md,
// the release number in
// ../../end-goal/how-the-factory-works/06-releases/04-the-release-number.md, an
// ordinal per service that orders builds and names rollback targets, and what
// the queue reads before it mints, in
// ../../end-goal/how-the-factory-works/05-environments/05-what-the-queue-reads-before-it-mints.md.
package release
