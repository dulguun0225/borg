// Package area owns the area record: a part of the software an owner grouped,
// what it lies inside, the hazard severity declared on it, and the item-size
// target authored on it.
//
// # The code
//
// schema.go is [Table], [IDPrefix], [FormatVersion] and [DDL]. writer.go is
// [Area], [Inside], [Writer] and [NewWriter] with [Writer.Declare] and
// [SetItemSizeTarget], and the reads [Get], [ByName] and [Chain]. hazard.go is
// [Grade], [Grades], [Hazard] and [SeverityInForce]. The tests are db_test.go,
// every one of them against the database.
//
// An area is free to cut across services, so an item names its area as a field
// of its own rather than reaching one through the service it names. What the
// area is for is being named by something else: a safeguard or a scope drawn on
// an area reaches every item in it, and the item-size target is per area.
//
// Areas form a chain, each naming the area or the project it lies inside, and
// [Chain] walks one from an area to the project it ends at, refusing a chain that
// cycles. A safeguard drawn on any area in that chain reaches an item in the
// narrowest, which is why a mechanism a safeguard binds asks for the chain rather
// than for the one area the item names. The chain ends at a project and nowhere
// else, which [Inside] is what enforces: exactly one of the two ids is set.
//
// The hazard severity is the same shape one level up: the value in force for an
// item is the highest grade named anywhere on its chain, which is what
// [SeverityInForce] reads, so declaring a finer area never lowers it. An area
// graded irreversible names its hazardous operation and the bound the owner
// authored on it, and the grade is not written without the two.
//
// The item-size target's unit is the count of its intent's requirements an item
// answers — the field decomposition writes per item.
//
// Who may write what: [Writer.Declare] is an owner declaring an area at Factory,
// and [SetItemSizeTarget] is an owner authoring the target on one, called by
// package policy inside the transaction that appends the policy version — the
// arrangement package criterion already has with the artifact store, and for the
// same reason: the field and the version commit together or not at all.
// Decomposition reads an area and writes none.
//
// What is not built: nothing re-grades an area after it is declared, the design
// naming Factory as where the grade is written and saying nothing about a second
// write; and the three readers of the value in force — the Implementation gate,
// the rollout strategy, and the vector a gate firing writes — are elsewhere.
//
// What defines it:
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/02-what-an-item-names.md,
// which sets the one writer, the chain, and an area cutting across services;
// the hazard severity, the hazardous operation and the bound are
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/03-hazard-severity.md;
// the project the chain ends at is
// ../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md;
// the target it holds and its unit are
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/README.md and the row
// in ../../end-goal/how-the-factory-works/09-gate-policy/01-what-is-in-it.md.
package area
