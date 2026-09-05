// Package contractcheck is enforcement: the mechanical answer to what a change
// breaks, and everything that reads the graph a contract and a consumer
// contract make.
//
// contractcheck.go is the component itself: [Check] and [New], composed with
// the [Checkout] and [Exchanges] seams, plus the [Candidate] a check is asked
// about and [Actor], the actor its one write is made as. The three files below
// are one question each.
//
// The tests are against the database: fixtures_test.go holds the graph and the
// fakes the others build on, db_test.go is the composition [New] refuses, and
// enforce_test.go, deprecation_test.go and inforce_test.go are the three
// questions, one file each.
//
// [Check.Enforce], in check.go, is the whole of what the merge row asks about
// one [Candidate], and it holds two baselines because they are different
// questions: a producer's own diff runs against the contract version its
// service's current release publishes, and a consumer contract is decided
// against the version its producer's newest release publishes. Neither baseline
// is written down and neither produces a record — both are computed at the
// moment the gate fires, and the merge queue calls this again at
// re-verification with [Actor] as the actor. [Checked] is what it found,
// [Broken] the producer's side and [Unmet] and [Unsatisfied] the consumers'.
//
// [Check.Deprecated], in deprecation.go, is the deprecation list: a [Marked]
// per marked element with [Blocking] naming what still holds it, and
// [Blocking.Blocked] is the rejection. A breaking diff and the list are one
// check, so a breaking change passes exactly when nothing still names the
// elements it breaks — no consumer contract in force, and no safeguard's
// predicate. A safeguard's predicate is told apart from a derived consumer
// contract, because what clears it is a withdrawal rather than a release.
// [Check.RaiseRemovals] is one pass over every marked element, taking a removal
// intent in for each whose derived consumer contracts are gone, deduplicated by
// [RemovalStatement] and not by a record saying it has fired.
//
// [Check.ConsumerContractsInForce], in inforce.go, is for one service the
// predicates derived by the items of every release from its
// [Check.LastKnownGood] to its newest, as an [InForce]; [Check.Binding] is that
// range applied to every service that has ever declared against one producer,
// read off the consumer contracts themselves rather than off every service
// there is. The last known-good release is computed here rather than in package
// healthmonitor, which computes a rollback's target from the same windows and
// answers a different question, and neither is in package window, which cannot
// read a release's number.
//
// Who may write what: this component owns no table. It writes one record — the
// removal intent, through [intent.Intake] — and everything else it does is a
// read. What it does not do is reject: it answers, and the caller gives that
// answer to [gate.Gate.AutoReject], which is the one thing that closes a
// firing. What it does to a checkout and what it observes of a run are behind
// [Checkout] and [Exchanges], which whatever composes the deploy agent
// implements and [New] takes.
//
// What defines it: the diff, a breaking one being a rejection at the Merge to
// master gate, and who is affected being a query are
// ../../end-goal/how-the-factory-works/07-contracts/04-enforcement.md; the two
// baselines, the range consumer contracts in force are read over, and the
// safeguard's predicate are
// ../../end-goal/how-the-factory-works/07-contracts/06-what-a-consumer-declares.md; the
// list and the detector are
// ../../end-goal/how-the-factory-works/07-contracts/08-deprecation.md; the three items
// a breaking change is are
// ../../end-goal/how-the-factory-works/07-contracts/02-no-single-item-may-break-a-contract.md;
// the store's forward promise and its own past as the consumer are
// ../../end-goal/how-the-factory-works/07-contracts/09-the-store-is-a-contract-too.md;
// and the last known-good release is
// ../../end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md.
package contractcheck
