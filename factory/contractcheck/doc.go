// Package contractcheck is enforcement: the mechanical answer to what a change
// breaks, and everything that reads the graph a contract and a consumer
// contract make.
//
// contractcheck.go is the component itself: [Check] and [New], composed with
// the [Checkout], [Exchanges], [StoreState] and [Backfills] seams, plus the
// [Candidate] a check is asked about and [Actor], the actor its two writes —
// the brownout intent and the removal intent — are made as. The five files
// below are one question each, checked.go being the shape [Check.Enforce]
// returns and not a sixth question.
//
// The tests are against the database: fixtures_test.go holds the graph the
// others build on and fakes_test.go the four seams it is composed with,
// db_test.go is the composition [New] refuses, and
// enforce_test.go, deprecation_test.go, inforce_test.go and store_test.go are
// the four questions, one file each.
//
// [Check.Enforce], in check.go, is the whole of what the merge row asks about
// one [Candidate], and it holds two baselines because they are different
// questions: a producer's own diff runs against the contract version its
// service's current release publishes, and a consumer contract is decided
// against the version its producer's newest release publishes. Neither baseline
// is written down and neither produces a record — both are computed at the
// moment the gate fires, and the merge queue calls this again at
// re-verification with [Actor] as the actor. [Checked], in checked.go, is what
// it found, [Broken] the producer's side and [Unmet] and [Unsatisfied] the
// consumers'.
//
// [Check.Deprecated], in deprecation.go, is the deprecation list: a [Marked]
// per marked element with [Blocking] naming what still holds it, and
// [Blocking.Blocked] is the rejection. A breaking diff and the list are one
// check, so a breaking change passes exactly when nothing still names the
// elements it breaks — no consumer contract in force, and no safeguard's
// predicate. A safeguard's predicate is told apart from a derived consumer
// contract, because what clears it is a withdrawal rather than a release.
// [Check.Raise], also in deprecation.go, is the detector: one pass over every
// marked element, raising the brownout where [Marked.Empty] and raising the
// removal only once that brownout's own window — read in brownout.go by
// walking a release back to the intent its item names, on the same evidence
// key — has reached its cap uncrossed having received volume. It raises
// neither again for an element whose brownout failed, deduplicating both
// raises by the evidence [intent.OnEvidence] reads and not by a record saying
// either has fired.
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
// [Check.storeRule], in store.go, is the store migration's own three items: the
// middle three have an empty diff by construction and are decided against
// [StoreState.Rows] instead, and the two the candidate environment exercises —
// applying the change twice through [StoreState.AppliedTwice] and, where it
// destroys stored data, taking and verifying a snapshot through
// [StoreState.Snapshot] — are read off that environment's own run. The double
// application is asked for only where [Checkout.DeclaresSchemaChange] says the
// build declares one: a store contract's form moves whenever the code deriving
// it moves, and a build can move it with no change for a deploy to apply. [Migration]
// is what it found and [Migration.Blocked] the rejection; [Waiting] is an
// element whose backfill no deploy record marks complete through [Backfills],
// which blocks the item that moves reads to it and the drop after it until one
// does.
//
// Who may write what: this component owns no table. It writes two records —
// the brownout intent and the removal intent, both through [intent.Intake] —
// and everything else it does is a read. What it does not do is reject: it
// answers, and the caller gives that answer to [gate.Gate.AutoReject], which is
// the one thing that closes a firing. What it does to a checkout, what it
// observes of a run, what the candidate environment's own store holds, and
// which backfills a deploy record marks complete are behind [Checkout],
// [Exchanges], [StoreState] and [Backfills], which whatever composes the
// deployer implements and [New] takes.
//
// What defines it: the diff, a breaking one being a rejection at the Merge to
// master gate, and who is affected being a query are
// ../../end-goal/how-the-factory-works/07-contracts/04-enforcement.md; the two
// baselines, the range consumer contracts in force are read over, and the
// safeguard's predicate are
// ../../end-goal/how-the-factory-works/07-contracts/06-what-a-consumer-declares.md; the
// list, the brownout and the detector are
// ../../end-goal/how-the-factory-works/07-contracts/08-deprecation.md; the four items
// a breaking change is, five for a store, are
// ../../end-goal/how-the-factory-works/07-contracts/02-no-single-item-may-break-a-contract.md;
// the store's forward promise, its own past as the consumer, and its migration's
// middle items are
// ../../end-goal/how-the-factory-works/07-contracts/09-the-store-is-a-contract-too.md;
// a schema change and its snapshot are
// ../../end-goal/how-the-factory-works/06-releases/05-the-deploy-record/01-a-schema-change.md;
// and the last known-good release is
// ../../end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md.
package contractcheck
