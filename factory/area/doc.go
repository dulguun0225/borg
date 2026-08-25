// Package area owns the area record: a part of the software an owner grouped,
// the area it lies inside, and the item-size target authored on it.
//
// An area is free to cut across services, so an item names its area as a field
// of its own rather than reaching one through the service it names. What the
// area is for is being named by something else: a safeguard or a scope drawn on
// an area reaches every item in it, and the item-size target is per area.
//
// # The chain
//
// Areas form a chain, each naming the area it lies inside, and [Chain] walks
// one from an area to the outermost. A safeguard drawn on any area in that
// chain reaches an item in the narrowest, which is why the walk exists and why
// a mechanism a safeguard binds asks for the chain rather than for the one area
// the item names. The design has the chain end at the project the area lies
// inside; there is no project record until the screens are built, so a chain
// here ends at an area whose Inside is empty. What that costs is that an owner
// who declares an area cannot yet say which project it belongs to, so nothing
// checks that an item's area and its service are in one project.
//
// Who may write what: [Writer] is an owner declaring an area at Factory, and
// [SetItemSizeTarget] is an owner authoring the target on one, called by
// package policy inside the transaction that appends the policy version — the
// arrangement package criterion already has with the artifact store, and for
// the same reason: the field and the version commit together or not at all.
// Decomposition reads an area and writes none.
//
// What defines it:
// ../../end-goal/how-humans-do-it/02-intent-into-items/03-decomposition/02-what-an-item-names.md,
// which sets the one writer, the chain, and an area cutting across services;
// the target it holds is
// ../../end-goal/how-humans-do-it/02-intent-into-items/03-decomposition/README.md and the row
// in ../../end-goal/how-humans-do-it/09-gate-policy.md#what-is-in-it.
package area
