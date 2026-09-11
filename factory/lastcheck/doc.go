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
// record: [PlatformPass], [Writer.RecordPlatformPass] and [PlatformPassOf].
// schema.go is [Table], [IDPrefix], [FormatVersion] and [DDL].
//
// The tests are db_test.go, every one of them against the database.
//
// One record type, overwritten per component and subject: an insert that
// conflicts on the pair and updates. The subject is a service id, a target
// address, a production environment record's id, or empty on the record a
// component keeps for itself.
// The payload is the counts the writer reports, stored as the text the writer
// wrote and read here for one shape only: the deployer's per-platform record,
// whose three counts the design names and a screen reads back, which is what
// [PlatformPass] is and why platform.go departs from the rest of the file.
//
// [LastCheck.NewestRecord] is beside the payload and not in it, because the
// component that writes it reads it back: the health monitor writes the newest
// time the emission holds a record for the service, and a read whose newest
// record is older than the interval the same record carries is no volume rather
// than a low one. A value inside the payload would be one this package stores as
// text and its writer parses out of prose.
//
// # Who may write what
//
// [Writer.Record] refuses an actor that is not a component, and so does the
// CHECK: a last check is the writing component's own record of its own pass,
// about the component and never about the work. Seven components write one
// here, each its own rows. The eighth the design names is the drift
// detector's, kept in package driftdetector's own store, which no factory
// component may write — so [Components] does not list it and the CHECK
// refuses it.
//
// Of the seven, four are wired. The health monitor writes one per service on
// every pass it makes over that service's windows; the notifier writes its
// single one for itself on the pass that reads the drift detector's store; the
// deployer writes two kinds of its own, both on every production deploy: one
// per target of a persistent environment, keyed by the target's own address,
// through deploy.RecordTargetCheck, and one per platform a production
// environment record declares, keyed by that record's own id and not by the
// platform's name, through [Writer.RecordPlatformPass] here — the sole writer
// of that record, composing the payload from the three counts the design
// names rather than taking it as text; and package contractcheck's pass over
// the deprecation list writes its single one for itself on every pass of
// [contractcheck.Check.Raise], the shape this and the notifier's own already
// have. The other three are not
// wired: a single record each for the pass over the constraints in force, the
// pass over the advisory feed, and dispatch's pass over a fleet proposal,
// three passes this milestone does not build. Each is wired by the dispatch
// that builds its writer.
//
// What defines it:
// ../../end-goal/how-the-factory-works/08-operations/08-drift-detection.md
// (C2153), which sets the shape every last check record has — the interval on
// the record, the further pass owed, and the third comparison that reads them
// all;
//
// ../../end-goal/records.md (C2826) for the eight components each writing its
// own;
//
// ../../end-goal/one-process.md (C2746, C2757) for why the health monitor keeps
// one per service rather than one for itself; and
// ../../end-goal/how-the-factory-works/05-environments/02-an-environment-per-candidate/03-room-and-what-an-environment-costs.md
// (C1507, C1508, C1511) for the three counts the deployer's platform record
// reports.
//
// The deployer's last check making the state readable is
// ../../end-goal/how-the-factory-works/06-releases/06-rollback.md (C1756); the
// newest record the health monitor writes onto its own is a field here and a
// reading the health monitor makes, cited there.
package lastcheck
