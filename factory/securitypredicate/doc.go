// Package securitypredicate is the factory's own list of security predicates
// and the derivations that decide them against a build.
//
// It is the second of the two lists a predicate is drawn from. The first is the
// list of allowed predicate kinds, which is gate policy's and which a consumer
// contract picks from; this one is the factory's, shipped as content of the
// product, decided per toolchain against the build, and an owner may only extend
// it.
//
// # The code
//
// list.go is [Kind], [List], [Lists], [Go], [ForToolchain] and [ToolchainGo]:
// what this factory version ships, one list per toolchain it covers, each
// naming the version that shipped it. decide.go is [Checkout], [Result],
// [Decided] with [Decided.Rejected], [Decided.CouldNotBeDerived] and
// [Decided.Why], and [Decide], which runs one derivation per kind over the
// checkout. decide_test.go is the tests, which need no database.
//
// [Go] ships no kind. What a security predicate asserts is not stated in the
// Markdown below, so the content is the one thing here that is not built: a kind
// added to that list is decided in decide.go's derivation set in the same
// commit, and a kind added to a list without a derivation for it reads as could
// not derive rather than as satisfied.
//
// The list is versioned by the factory version that shipped it, which is what
// the install event names; the install event itself is not built.
//
// Who may write what: this package owns no table, writes no record, reaches no
// database, and imports nothing inside this module. It reads a checkout and
// answers. What the answer becomes is the caller's: a kind that did not hold is
// gate.AutoRejectedBySecurityPredicate at the Merge to master row, and a
// derivation that could not derive is gate.CouldNotDeriveSecurityPredicate on
// that row's firing, which puts a human there rather than rejecting.
//
// What defines it: the second list, what it owns and how it is versioned, are
// ../../end-goal/how-the-factory-works/07-contracts/06-what-a-consumer-declares.md;
// the list being decided against the candidate's run with one set of derivations
// per toolchain is
// ../../end-goal/how-the-factory-works/05-environments/04-what-the-candidate-environment-decides/README.md;
// and what a security predicate does at the merge row, rejecting on the terms an
// undecided criterion does and putting a human there where it could not derive,
// is
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/07-merge-to-master.md.
package securitypredicate
