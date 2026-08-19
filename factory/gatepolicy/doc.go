// Package gatepolicy is the vocabulary of everything an owner authors: the
// parameters, what each one's value means, the record its scope names, and the
// direction a pin on it points. It owns no table and writes nothing, the way
// package record owns none — what it holds is the one list two packages would
// otherwise each keep a copy of, package pin naming a parameter on a record and
// package policy resolving one.
//
// # Seven rows, eight parameters
//
// Gate policy is seven rows and this package names eight parameters, because one
// row carries two values: the watch window's size and the confidence it
// requires are authored together and pinned in opposite directions — a ceiling
// over the size, a floor under the confidence — which is what makes them two
// parameters of one row rather than one parameter. [Row] is what a printer
// groups by and what TestSevenRows counts.
//
// # A pin can only add
//
// The direction differs per parameter and points the same way in each: toward
// more protection and never toward less. A pin is a bound and not a precedence
// — the value in force is what an owner authored where they authored one and
// what the score supplies otherwise, clamped by the pin, so a pin never
// replaces a value already narrower than itself. Read as a precedence, a
// pinned ceiling over K of five would override an authored two and raise the
// number, which is a pin adding throughput and removing safety.
//
// The risk threshold's direction is the one that is not arithmetic: an owner
// pinning it adds a human at the gate rather than moving the number the gate
// compares against, which is why [DirectionAddsAHuman] carries no bound. The
// catalog's floor is the other: a floor under a list is the union of the two,
// because a kind of assertion added is coverage added and one removed would
// invalidate declarations already ratified at a gate.
//
// What defines it:
// ../../end-goal/how-humans-do-it/09-gate-policy.md#what-is-in-it for the rows
// and ../../end-goal/how-humans-do-it/09-gate-policy.md#one-shape-across-all-of-them
// for the scope of each and the direction its pin takes.
package gatepolicy
