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
// ../../end-goal/how-the-factory-works/09-gate-policy/01-what-is-in-it.md
// (C2187, C2188, C2189, C2190, C2191, C2192, C2193, C2195, C2196, C2197,
// C2198), and the scope of each, the key it is held under, and the direction
// its safeguard takes are
// ../../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md
// (C2199, C2200, C2201, C2202, C2203, C2204, C2205, C2206, C2207, C2208, C2226,
// C2228, C2229, C2232, C2233, C2251, C2252, C2253, C2257, C2258, C2259, C2260,
// C2261, C2262, C2263, C2265, C2266, C2269, C2270, C2271, C2272).
//
// What is authored and not among the eleven is
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/01-authored-and-not-among-the-eleven.md
// (C2276, C2277, C2278, C2279, C2280, C2281, C2283, C2284, C2285, C2286, C2289,
// C2293, C2294, C2295) and
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/02-retention.md
// (C2296, C2297, C2298, C2300, C2302, C2304, C2305, C2307).
//
// The strategy default being production's environment record's alone is
// ../../end-goal/how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md,
// and the change freeze is
// ../../end-goal/how-the-factory-works/09-gate-policy/04-stopping-the-factory.md
// (C2332, C2350, C2351, C2352, C2353).
//
// The predicate kinds are
// ../../end-goal/how-the-factory-works/07-contracts/06-what-a-consumer-declares.md
// (C1798, C1799, C1800, C1802, C1818), and the quantities are
// ../../end-goal/how-the-factory-works/08-operations/01-the-health-monitor.md
// (C1937, C1938, C1939, C1943, C1944, C1947, C1948, C1949, C1967, C1970,
// C1972).
package gatepolicy
