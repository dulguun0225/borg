// Package release owns the release record and the number: one row per
// release, written once at the fast-forward that puts the item's commit on
// master, and never written again.
//
// # The files
//
// writer.go is [Release], [Writer] and [NewWriter] with [Writer.Mint] and
// [Writer.MintWith], and [Get]. count.go is the other reads — [All],
// [Highest], [ForItem], [Above], [Below], [Between] — with the counts a factor
// is read from, [CountForItemsSince], [CountForService] and
// [ItemsWithRelease]. schema.go is [Table], [IDPrefix], [AdvisoryLockKey] and
// [DDL].
//
// db_test.go is the tests against the database, and lock_test.go is the one
// subject that needs none: the advisory lock key recomputed from the name it
// is derived from.
//
// The record is where the graph joins. It names the item that caused the
// release and the build it is made of; every deploy of it names the release,
// and the gate decisions name the item and the build and never the release.
//
// Nothing here names a contract version. A contract version names the release
// and copies its number, so a release names what it publishes as an inbound
// edge, and a column would be the same fact twice on a record that has no
// update. [Writer.MintWith] is how the versions are written instead: the merge
// queue writes them inside this transaction, so a number and the versions its
// release publishes commit together or not at all.
//
// # The number
//
// The number is an ordinal, per service, minted with the record in one
// transaction: [Writer.Mint] takes a per-service advisory lock, reads the
// highest number the service has, and inserts one above it, so two mints for
// one service serialise and the numbers have no gap and no duplicate. The
// unique constraint on (service_id, number) is the same rule in the store,
// refusing what a skipped lock would produce. Numbers are never reused: a
// rolled-back release keeps its number, and the fix that follows takes the
// next one.
//
// [Highest] is master's head — the commit of the service's highest-numbered
// release — and [Above], [Below], [Between], [ForItem], and [All] are the
// other reads.
//
// Who may write what: [Writer.Mint] and [Writer.MintWith] insert into release
// and update and delete nothing. Written once is a property of the API — there
// is no update method.
//
// What defines it: the release record in
// ../../end-goal/how-the-factory-works/06-releases/02-the-release-record.md — written
// at the fast-forward, the record the whole graph joins on — and the release
// number in ../../end-goal/how-the-factory-works/06-releases/04-the-release-number.md,
// an ordinal per service that orders builds and names rollback targets.
package release
