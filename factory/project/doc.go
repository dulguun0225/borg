// Package project owns the project record: an owner's widest grouping of work,
// and the root every area chain ends at.
//
// # The code
//
// schema.go is [Table], [IDPrefix], [FormatVersion] and [DDL], whose unique
// constraint on the name refuses a second project of one name. writer.go is
// [Project], [Writer] and [NewWriter] with [Writer.Create], the transaction-taking
// [Insert], and the reads [Get], [ByName] and [All]. The tests are db_test.go,
// every one of them against the database.
//
// The record holds its identity and nothing an owner authors: what is authored
// per project is a field of production's environment record, of a constraint, of
// a safeguard, or of a fleet entry, each naming the project.
//
// Who may write what: one writer, an owner at Factory. [Insert] is what package
// policy calls, so the write that creates a project and the write that creates
// production's environment for it are one event; that composition is policy's and
// is not built here. [Writer.Create] is the same write in a transaction of its
// own, which is what a test and the command-line interface use. A project is
// never deleted, so an area, a constraint, a safeguard, or a scope naming one
// never points at nothing, and this package has no delete.
//
// What defines it: the record, its one writer, and production's environment
// written in the same event are
// ../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md.
// That it is the root every area chain ends at is
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/02-what-an-item-names.md,
// and that a service names one as part of its identity is
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/README.md.
package project
