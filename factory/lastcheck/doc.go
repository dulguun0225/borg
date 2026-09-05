// Package lastcheck owns the last check: one component's record of its own most
// recent pass, which is what makes a component that stopped visible rather than
// silent.
//
// # The files
//
// lastcheck.go is [LastCheck] with [LastCheck.FurtherPassOwed] and
// [LastCheck.Stale], the [Components] that write one and the constant per
// component, [Writer] and [NewWriter] with [Writer.Record], and the reads [All],
// [ForComponent], [Get] and [Stale]. schema.go is [Table], [IDPrefix],
// [FormatVersion] and [DDL].
//
// The tests are db_test.go, every one of them against the database.
//
// One record type, overwritten per component and subject: an insert that
// conflicts on the pair and updates. The subject is a service id, a target
// address, a platform name, or empty on the record a component keeps for itself.
// The payload is the counts the writer reports, stored as the text the writer
// wrote and never read here.
//
// # Who may write what
//
// [Writer.Record] refuses an actor that is not a component, and so does the
// CHECK: a last check is the writing component's own record of its own pass,
// about the component and never about the work. Six components write one here,
// each its own rows. The seventh the design names is the drift detector's, kept
// in package driftdetector's own store, which no factory component may write —
// so [Components] does not list it and the CHECK refuses it.
//
// Of the six, none is wired yet. The health monitor writes one per service, the
// deployer one per target of a persistent environment and one per platform a
// production environment record declares, the notifier a single one for itself,
// and the pass over the constraints in force, the pass over the advisory feed
// and dispatch's pass over a fleet proposal a single one each — the last three
// being passes this milestone does not build. Each is wired by the dispatch that
// builds its writer.
//
// What defines it:
// ../../end-goal/how-the-factory-works/08-operations/08-drift-detection.md,
// which sets the shape every last check record has — the interval on the record,
// the further pass owed, and the third comparison that reads them all;
// ../../end-goal/records.md for the seven components each writing its own;
// ../../end-goal/one-process.md for why the health monitor keeps one per service
// rather than one for itself; and
// ../../end-goal/how-the-factory-works/05-environments/02-an-environment-per-candidate/03-room-and-what-an-environment-costs.md
// for the three counts the deployer's platform record reports.
package lastcheck
