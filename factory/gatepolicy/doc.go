// Package gatepolicy is the vocabulary of everything an owner authors: the
// parameters, what each one's value means, the record its scope names, and the
// direction a safeguard on it points. It owns no table and writes nothing, the way
// package record owns none — what it holds is the one list two packages would
// otherwise each keep a copy of, package safeguard naming a parameter on a record and
// package policy resolving one.
//
// # Seven rows, eight parameters, and one only a safeguard sets
//
// Gate policy is seven rows and this package names eight parameters, because one
// row carries two values: the analysis window's size and the confidence it
// requires are authored together and bounded in opposite directions — a ceiling
// over the size, a floor under the confidence — which is what makes them two
// parameters of one row rather than one parameter. [Row] is what a printer
// groups by and what TestSevenRows counts.
//
// [SafeguardOnly] is beside those eight and not among them: a safeguard's
// predicate is a safeguard on one element of a contract and nobody authors a value
// for it, so it belongs to the vocabulary of safeguards rather than to the rows of
// gate policy. Listing it with the eight would make the count of rows eight while
// changing nothing about what an owner may write, so it is a list of its own and
// [Define] reads both.
//
// # A safeguard can only add
//
// The direction differs per parameter and points the same way in each: toward
// more protection and never toward less. A safeguard is a bound and not a
// precedence — the value in force is what an owner authored where they authored
// one and what the score supplies otherwise, clamped by the safeguard, so a
// safeguard never replaces a value already narrower than itself. Read as a
// precedence, a ceiling of five over a window limit would override an authored two
// and raise the number, which is a safeguard adding throughput and removing
// safety.
//
// Two directions are not arithmetic and the second is new here. The risk
// threshold's is the first: a safeguard on it adds a human at the gate rather than
// moving the number the gate compares against, which is why [DirectionAddsAHuman]
// carries no bound. The allowed kinds' floor is the second: a floor under a list is
// the union of the two, because a kind of assertion added is coverage added and one
// removed would invalidate consumer contracts already ratified at a gate. A
// safeguard's predicate is the third and it is the same shape as the allowed kinds'
// one level down — the predicates in force on an element are the ones derived plus
// the ones a safeguard added, and a safeguard adds to that set and takes nothing
// out of it. [SafeguardOnly] says why that direction is derived rather than read
// off the design's own list.
//
// # The predicate kinds
//
// [PredicateKinds] is the other list this package holds, and it is vocabulary for
// the same reason the parameters are: package consumercontract derives a
// predicate, package policy resolves the list those kinds are the unauthored value
// of, and enforcement decides one. predicate.go says what each kind asserts, which
// of them can be decided against a form with no run to observe, and what a kind
// outside the five means.
//
// What defines it:
// ../../end-goal/how-humans-do-it/09-gate-policy.md#what-is-in-it for the rows
// and ../../end-goal/how-humans-do-it/09-gate-policy.md#one-shape-across-all-of-them
// for the scope of each and the direction its safeguard takes. The predicate kinds
// are
// ../../end-goal/how-humans-do-it/07-contracts.md#what-a-consumer-declares.
package gatepolicy
