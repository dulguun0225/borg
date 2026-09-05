// Package area owns the area record: a part of the software an owner grouped,
// the area it lies inside, and the item-size target authored on it.
//
// writer.go is [Area], [Writer] and [NewWriter] with [Writer.Declare] and
// [SetItemSizeTarget], and the reads [Get], [ByName] and [Chain]; schema.go is
// [Table], [IDPrefix] and [DDL]. The tests are db_test.go, every one of them
// against the database.
//
// An area is free to cut across services, so an item names its area as a field
// of its own rather than reaching one through the service it names. What the
// area is for is being named by something else: a safeguard or a scope drawn on
// an area reaches every item in it, and the item-size target is per area.
//
// Areas form a chain, each naming the area it lies inside, and [Chain] walks one
// from an area to the outermost, refusing a chain that cycles. A safeguard drawn
// on any area in that chain reaches an item in the narrowest, which is why a
// mechanism a safeguard binds asks for the chain rather than for the one area
// the item names. A chain here ends at an area whose Inside is empty, there
// being no project record for it to end at. [ByName] and [Get] are the other
// reads.
//
// Who may write what: [Writer.Declare] is an owner declaring an area at Factory,
// and [SetItemSizeTarget] is an owner authoring the target on one, called by
// package policy inside the transaction that appends the policy version — the
// arrangement package criterion already has with the artifact store, and for the
// same reason: the field and the version commit together or not at all.
// Decomposition reads an area and writes none.
//
// What defines it:
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/02-what-an-item-names.md,
// which sets the one writer, the chain, and an area cutting across services;
// the target it holds is
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/README.md and the row
// in ../../end-goal/how-the-factory-works/09-gate-policy/01-what-is-in-it.md.
package area
