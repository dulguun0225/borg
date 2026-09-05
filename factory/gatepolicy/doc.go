// Package gatepolicy is the vocabulary of everything an owner authors: the
// parameters, what each one's value means, the record its scope names, and the
// direction a safeguard on it points. It owns no table, reaches no database, and
// writes nothing — it holds the one list package safeguard and package policy
// would otherwise each keep a copy of.
//
// parameter.go is [Parameter] and its [Definition]: the [Kind] its value takes,
// the [Scope] whose record holds it, the [Direction] a safeguard points, the
// unit, and the gate-policy row it belongs to. [Definitions] is the eight
// parameters of the seven rows, one row carrying two — the analysis window's
// size and the confidence it requires, authored together and bounded in opposite
// directions — and [Rows] is what a printer groups by. [SafeguardOnly] is a list
// of its own, holding the one parameter nobody authors, and [Define] reads both
// lists and returns [ErrUnknown] for anything in neither.
//
// clamp.go is [Clamp] and [ClampList]: a bound narrows the value in force and
// never replaces one already narrower than itself, and a floor under a list is
// the union of the two. [DirectionAddsAHuman] carries no bound and nothing
// clamps it. authored.go is [Authored] — a number and whether an owner authored
// one at all, absent being different from zero — with [Authored.Or] for what the
// score supplies where they authored none.
//
// predicate.go is [PredicateKind] and [PredicateKinds], the five kinds of
// assertion a consumer contract may draw from: [DecidablePredicate] refuses a
// kind outside them with [ErrPredicateKindUnknown], [PredicateKind.TakesAnArgument]
// and [PredicateKind.DecidableAgainstAForm] say what each kind needs and which of
// them can be decided against a form with no run to observe, and
// [AllowedPredicateKindNames] is the unauthored value of the list package policy
// resolves.
//
// Who may write what: nothing here writes. Every value this package names is
// written by the package that owns the record the parameter is a field of.
//
// What defines it: the rows are
// ../../end-goal/how-the-factory-works/09-gate-policy/01-what-is-in-it.md, and the scope
// of each and the direction its safeguard takes are
// ../../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md.
// The predicate kinds are
// ../../end-goal/how-the-factory-works/07-contracts/06-what-a-consumer-declares.md.
package gatepolicy
