// Package release owns the release record and the number: one row per
// release, written once at the fast-forward that puts the item's commit on
// master, and never written again.
//
// The record is where the graph joins. It names the item that caused the
// release and the build it is made of; every deploy of it names the release,
// and the gate decisions name the item and the build and never the release.
//
// The contract versions the design puts on the record have no column here, and
// this package predicted one from M1 until contracts were built. The prediction was
// wrong: a contract version names the release and copies its number, so "the
// release names the contract versions it publishes" is the inbound edge every
// deploy record of a release already is, and a column would be the same fact twice
// on a record that has no update. What arrived instead is [Writer.MintWith] — the
// versions are written by the merge queue inside this transaction, so a number and
// the versions its release publishes commit together or not at all.
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
// The design gives the fast-forward to the merge queue, whose per-service
// ordering is what stops two merges taking one number. The queue is M3, and
// the caller today is M1's crude path — the lock does for that caller what
// the queue's ordering will do, and costs nothing when the queue arrives,
// because a serialised caller waits on a lock nobody else holds.
//
// Who may write what: [Writer.Mint] inserts into release and updates and
// deletes nothing. Written once is a property of the API — there is no update
// method.
//
// What defines it: the release record in
// ../../end-goal/how-humans-do-it/06-releases.md#the-release-record — written
// at the fast-forward, the record the whole graph joins on — and the number
// in ../../end-goal/how-humans-do-it/06-releases.md#the-number, an ordinal
// per service that orders builds and names rollback targets and does nothing
// else.
package release
