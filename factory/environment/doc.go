// Package environment owns the environment record: where software runs, as a
// record rather than a name in code.
//
// One record type with a kind fixed at creation. The design has three —
// production, a deploy target a customer defines, and a candidate's own — and
// the CHECK in [DDL] lists the one this milestone writes, production, which
// every gate row of the default path reads its threshold from. The candidate
// kind is created by the deploy agent at the gate that decides its deploy and
// holds nothing an owner authored, so it arrives with the environment per
// candidate; a customer's own arrives with the surface an owner defines it on.
// What that costs is a CHECK widened once per kind, which is the arrangement
// package item's stage column already has.
//
// # What is on it
//
// The targets are the addresses a deploy is performed against, and they are a
// field rather than records of their own: nothing holds a reference to a target
// that has to survive an address change, a deploy record being keyed by service
// and environment. The credential a deploy is performed with is a
// [secretref.Ref], so the record names it and holds no value. The gate
// thresholds are a table of their own, one row per gate row an owner authored
// one for, because eight rows would otherwise be eight columns and a row a
// milestone has not built would be a column nothing writes.
//
// Who may write what: [Writer] is an owner at Factory, creating production's
// record at the creation of the project — which an owner does not choose,
// production existing everywhere — and [SetGateThreshold] is an owner
// authoring a threshold on one, called by package policy inside the transaction
// that appends the policy version. The score writes nothing here: where the
// threshold is unauthored the value in force is what the score supplies, and
// the supplied value is a field of the score's own record.
//
// What defines it:
// ../../end-goal/how-humans-do-it/05-environments.md#records-and-one-long-lived-branch,
// which sets the kind as the seam between writers, the targets as a field, and
// what a persistent kind holds; the threshold's scope is
// ../../end-goal/how-humans-do-it/09-gate-policy.md#one-shape-across-all-of-them.
package environment
