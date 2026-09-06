// Package contractcheck is enforcement: the mechanical answer to what a change
// breaks, and everything that reads the graph a contract and a consumer
// contract make.
//
// contractcheck.go is the component itself: [Check] and [New], composed with
// the [Checkout], [Exchanges] and [StoreState] seams, plus the [Candidate] a
// check is asked about and [Actor], the actor its two writes — the brownout
// intent and the removal intent — are made as. The six files
// below are one question each, checked.go being the shape [Check.Enforce]
// returns and not a seventh question.
//
// The tests are against the database: fixtures_test.go holds the graph the
// others build on and fakes_test.go the three seams it is composed with,
// db_test.go is the composition [New] refuses, and
// enforce_test.go, deprecation_test.go, inforce_test.go, store_test.go and
// composition_test.go are the questions, one file each.
//
// [Check.Enforce], in check.go, is the whole of what the merge row asks about
// one [Candidate], and it holds two baselines because they are different
// questions: a producer's own diff runs against the contract version its
// service's current release publishes, and a consumer contract is decided
// against the version its producer's newest release publishes. Neither baseline
// is written down and neither produces a record — both are computed at the
// moment the gate fires, and the merge queue calls this again at
// re-verification with [Actor] as the actor. What is running is read through
// check.go's serviceAddresses, which composition.go uses per producer too: the
// targets of the production environment a service record says it runs on, and
// every target of the environment where it names none. [Checked], in checked.go,
// is what it found, [Broken] the producer's side and [Unmet] and [Unsatisfied]
// the consumers'.
//
// [Check.Deprecated], in deprecation.go, is the deprecation list: a [Marked]
// per marked element, naming the consumer contracts and safeguards' predicates
// that still hold it and the [Unreadable] consumers — the partial derivations
// and the ones nobody could derive at all — that hold it whatever they declare;
// [Marked.Empty] is nothing holding it, which
// is what a breaking change and a removal each wait for. [Broken.Blocking],
// built by the diff in check.go, is what a rejection names, and it carries the
// same [Unreadable] pair, [Blocking.HeldByADerivation] being the same question
// [Marked.Empty] asks: the deprecation list and the diff's own list are one list
// read at two moments, so a removal reaching the merge row by any route waits on
// what the detector waits on. A breaking diff and the list are one
// check, so a breaking change passes exactly when nothing still names the
// elements it breaks — no consumer contract in force, no consumer whose
// derivation cannot be read as having stopped, and no safeguard's
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
// [Check.IsBrownout], in brownout.go, is that same walk in the other direction
// and is the reading the health monitor needs of this component: whether a
// release is a brownout of a marked element, as a [BrownoutOf]. A brownout's
// window runs to its cap rather than stopping where the boundary would allow,
// and it is the one window that reads more than the producer's own numbers, any
// service crossing the reading against its own recent history while it is open
// failing it. Both are the health monitor's to perform and it is not told which
// release is one yet: what it needs is this reading at the open, handed to it
// the way the held-out selection already is, so that the
// passed exit is unavailable to such a window the way it is to a held-out
// release, and again while it watches. cmd/factory reports the reading at the
// production deploy meanwhile. Until the health monitor takes it, a brownout's
// window can close passed before its cap, which is
// [Brownout.EstablishesNothing]: [Check.Raise] reports that as [Raised.Stalled]
// rather than passing over the element, no pass of the detector being able to
// raise the removal after such a close.
//
// [Check.ComposedFrom], in composition.go, is what a candidate's environment is
// composed from: the producers the candidate build's consumer contract names,
// and theirs through their current releases' consumer contracts, as a
// [Composed] each. What is not written is each entry's address for that
// environment — the composition record names a service and a release and has no
// field for one — so the addresses the entries reach a producer through are on
// the value and nothing stores them.
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
// application is asked for where [Checkout.DeclaresSchemaChange] says the build
// declares a change and where [Checkout.DeclaresBackfill] says it is a backfill,
// whose change is data and not form, and nowhere else: a store contract's form
// moves whenever the code deriving it moves, and a build can move it with no
// change for a deploy to apply. [Migration] is what it found and
// [Migration.Blocked] the rejection; [Waiting] is an element whose backfill no
// deploy record marks complete, read through deploy.BackfillComplete, which
// blocks the item that moves reads to it, the drop after it, and a constraint
// put on it while the form marks something, until one does. The other half of
// the constraint rule is in check.go: a not-null constraint or a domain check on
// a store's form is held by a declaration in force the new form rejects and not
// by the existence of one, which is what makes the design's ordinary path
// reachable. [contract.Diff] leaves both out of its breaking list for that
// reason, so check.go's atRisk adds them to what it asks about, and
// [Broken.Breaks] is what it found actually breaks — which is what mints
// [Broken.Next] and what says whether the change destroys stored data.
//
// Who may write what: this component owns no table. It writes two records —
// the brownout intent and the removal intent, both through [intent.Intake] —
// and everything else it does is a read. What it does not do is reject: it
// answers, and the caller gives that answer to [gate.Gate.AutoReject], which is
// the one thing that closes a firing. What it does to a checkout, what it
// observes of a run, and what the candidate environment's own store holds are
// behind [Checkout], [Exchanges] and [StoreState], which whatever composes the
// deployer implements and [New] takes. Which backfills a deploy record marks
// complete is not among them: it is a read of the deploy record, the way what is
// running is.
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
// the composition of a candidate's environment from what a consumer contract
// names is
// ../../end-goal/how-the-factory-works/07-contracts/11-which-producer-a-consumer-reaches.md;
// what a brownout's window runs to and reads is
// ../../end-goal/how-the-factory-works/08-operations/02-the-analysis-window.md;
// and the last known-good release is
// ../../end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md.
package contractcheck
