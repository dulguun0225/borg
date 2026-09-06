// Package lastcheck owns the last check: one component's record of its own most
// recent pass, which is what makes a component that stopped visible rather than
// silent.
//
// # The files
//
// lastcheck.go is [LastCheck] with [LastCheck.FurtherPassOwed] and
// [LastCheck.Stale], the [Components] that write one and the constant per
// component, [Writer] and [NewWriter] with [Writer.Record], and the reads [All],
// [ForComponent], [Get] and [Stale]. platform.go is the deployer's per-platform
// record: [PlatformPass] with [PlatformPass.Leaked],
// [Writer.RecordPlatformPass] and [PlatformPassOf]. schema.go is [Table],
// [IDPrefix], [FormatVersion] and [DDL].
//
// The tests are db_test.go, every one of them against the database.
//
// One record type, overwritten per component and subject: an insert that
// conflicts on the pair and updates. The subject is a service id, a target
// address, a platform name, or empty on the record a component keeps for itself.
// The payload is the counts the writer reports, stored as the text the writer
// wrote and read here for one shape only: the deployer's per-platform record,
// whose three counts the design names and a screen reads back, which is what
// [PlatformPass] is and why platform.go departs from the rest of the file.
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
// Of the six, three are wired. The health monitor writes one per service on
// every pass it makes over that service's windows; the notifier writes its
// single one for itself on the pass that reads the drift detector's store; and
// the deployer writes one per target of a persistent environment after a
// production deploy completes there. The other three are not: a single record
// each for the pass over the constraints in force, the pass over the advisory
// feed, and dispatch's pass over a fleet proposal, three passes this milestone
// does not build. Each is wired by the dispatch that builds its writer.
//
// The deployer's second record, one per platform a production environment
// record declares, is unwired too, and it has two writers, neither called:
// [Writer.RecordPlatformPass] here, which composes the three counts, and
// deploy.RecordPlatformCheck, which takes the payload as text. The pass is the
// deployer's, which lives in the command-line interface, and the composition
// owes one call per production environment record that declares a platform,
// each pass.
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
