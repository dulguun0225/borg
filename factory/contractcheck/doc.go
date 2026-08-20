// Package contractcheck is enforcement: the mechanical answer to what a change
// breaks, and everything that reads the graph a contract and a declaration make.
//
// # Two baselines, kept apart
//
// [Check.Enforce] is the whole of what the merge row asks about one candidate, and
// it holds both of the design's baselines because they are different questions. A
// producer's own diff runs against the contract version its service's current
// release publishes, because the promise is to what is running. A declaration is
// checked against the contract version its producer's newest release publishes,
// because that is what the consumer will meet.
//
// Neither is written down and neither produces a record: both are computed at the
// moment the gate fires, and the merge queue calls this again at re-verification
// with itself as the actor. So the race between the two resolves either way round
// it happens — a consumer that newly declares an element the producer is part-way
// through removing fails at its own gate, or the producer's removal candidate fails
// at its. What that costs is that a candidate which waited in the queue can fail on
// a baseline that moved after its own run passed, with no record of the earlier pass
// to point at.
//
// # A breaking diff and the deprecation list are one check
//
// "A breaking diff without the migration already shipped ahead of it is a
// rejection" is not two mechanisms. The list [Check.Deprecated] keeps is what
// emptied to allow the removal, so a breaking change passes exactly when nothing
// still names the elements it breaks: no declaration in force, and no pinned
// predicate. [Blocking] is that list per element, and [Blocking.Blocked] is the
// rejection.
//
// A pinned predicate blocks and is told apart from a derived declaration, because
// an owner placed it: the rejection names the pin and its author, and what clears it
// is a withdrawal rather than a release. What a stale pin costs is a full pass of
// the pipeline and an attempt spent on work the factory raised itself.
//
// # Declarations in force is a range over releases
//
// [Check.DeclarationsInForce] is, for one service, the predicates derived by the
// items of every release from its [Check.RestoreFloor] to its newest. One range
// answers what runs now, what has merged and will run, and what a rollback can
// still restore, and it is the same query for an interface and for a store.
//
// The floor is computed here rather than in package comparison, which computes a
// rollback's target from the same windows: the two coincide whenever windows close
// in the order they opened and differ exactly where they do not, and they answer
// different questions. Neither is in package window, which cannot read a release's
// number.
//
// [Check.Binding] is that range applied to every service that has ever declared
// against one producer — read off the declarations themselves rather than off every
// service there is. The producer is among them, because a service declares against
// its own store contract exactly as against another service's interface, and that
// consumer is the one a store's forward promise exists for.
//
// # The detector
//
// [Check.RaiseRemovals] is one pass over every marked element, taking a removal
// intent in for each whose derived declarations are gone. It is deduplicated by the
// statement and not by a record saying it has fired, which is the handle a revert
// already reaches the pipeline by. That is the whole of "nobody has to remember step
// three".
//
// # Who may write what
//
// This component owns no table. It writes one record — the removal intent, through
// [intent.Intake] — and everything else it does is a read. What it does not do is
// reject: it answers, and the caller gives that answer to [gate.Gate.AutoReject],
// which is the one thing that closes a firing.
//
// What it does to a checkout and what it observes of a run are behind [Checkout]
// and [Exchanges], which whatever composes the deploy agent implements. Both
// derivations are one toolchain's and the exchange is one substrate's, so a second
// toolchain or a second kind of target is two implementations of those interfaces
// and no change to the rest of enforcement. That is the arrangement package
// comparison already has for the quantity it reads and the rollback it asks for.
//
// What defines it: the diff, a breaking one being a rejection at the merge gate,
// and who is affected being a query are
// ../../end-goal/how-humans-do-it/07-contracts.md#enforcement; the two baselines,
// the range declarations in force are read over, and the pinned predicate are
// ../../end-goal/how-humans-do-it/07-contracts.md#what-a-consumer-declares; the
// list and the detector are
// ../../end-goal/how-humans-do-it/07-contracts.md#deprecation; the three items a
// breaking change is are
// ../../end-goal/how-humans-do-it/07-contracts.md#no-single-item-may-break-a-contract;
// the store's forward promise and its own past as the consumer are
// ../../end-goal/how-humans-do-it/07-contracts.md#the-store-is-a-contract-too; and
// the restore floor is
// ../../end-goal/how-humans-do-it/08-operations.md#overlapping-windows.
package contractcheck
