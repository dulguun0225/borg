// Package gatepolicy is the vocabulary of everything an owner authors: the
// parameters, what each one's value means, the record its scope names, the key
// its value is held under there, and the direction a safeguard on it points. It
// owns no table, reaches no database, and writes nothing.
//
// parameter.go is [Parameter] and its [Definition]: the [Kind] its value takes,
// the [Scope] whose record holds it, the [Key] it is held under there, the
// [Direction] a safeguard points, the unit, what it limits, and the gate-policy
// row it belongs to. [Define] reads the three lists and returns [ErrUnknown] for
// a name in none of them, and [Rows] is what a printer groups by.
//
// definitions.go is [Definitions], the thirteen parameters of the eleven rows,
// with the row names as constants. notamongtheeleven.go is [NotAmongTheEleven],
// what an owner authors on the factory-wide settings record, on production's
// environment record and on the service record that is not gate policy, and
// [SafeguardOnly], the two parameters nobody authors.
//
// quantity.go is [Quantity] and [Quantities], the numbers the health monitor
// reads, with [DecidableQuantity] refusing a name outside them: the analysis
// window's size and power are authored per quantity. strategy.go is [Strategy],
// [Strategies] and [DecidableStrategy], the same shape for the default an owner
// authors on production's environment record.
//
// clamp.go is [Clamp] and [ClampList]: a bound narrows the value in force and
// never replaces one already narrower than itself, and a floor under a list is
// the union of the two. [DirectionAddsAHuman] and [DirectionNone] carry no bound
// and nothing clamps them. authored.go is [Authored] — a number and whether an
// owner authored one at all, absent being different from zero — with
// [Authored.Or] for what the score supplies where they authored none.
//
// predicate.go is [PredicateKind] and [PredicateKinds], the nine kinds of
// assertion a consumer contract may draw from, five over what the consumer
// receives and four over what it sends: [DecidablePredicate] refuses a kind
// outside them with [ErrPredicateKindUnknown], [PredicateKind.Side],
// [PredicateKind.TakesAnArgument] and [PredicateKind.DecidableAgainstAForm] say
// what each kind is about and what it needs — the two a form cannot answer being
// the received domain and the received range — and [AllowedPredicateKindNames] is
// the unauthored value of the list package policy resolves.
//
// Who may write what: nothing here writes. Every value this package names is
// written by the package that owns the record the parameter is a field of.
//
// What defines it: the eleven rows are
// ../../end-goal/how-the-factory-works/09-gate-policy/01-what-is-in-it.md, and the scope
// of each, the key it is held under, and the direction its safeguard takes are
// ../../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md.
// What is authored and not among the eleven is
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/01-authored-and-not-among-the-eleven.md
// and ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/02-retention.md.
// The strategy default being production's environment record's alone is
// ../../end-goal/how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md,
// and the change freeze is
// ../../end-goal/how-the-factory-works/09-gate-policy/04-stopping-the-factory.md.
// The predicate kinds are
// ../../end-goal/how-the-factory-works/07-contracts/06-what-a-consumer-declares.md, and the
// quantities are
// ../../end-goal/how-the-factory-works/08-operations/01-the-health-monitor.md.
package gatepolicy
