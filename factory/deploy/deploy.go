package deploy

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// Strategy is how a release takes live traffic from the build it replaces. It
// attaches to a production deploy and to no other.
type Strategy string

const (
	// StrategyWithoutControl is all of the traffic, in place, with none of the
	// build it replaces left running.
	StrategyWithoutControl Strategy = "without_control"
	// StrategyWithControl is a share of the traffic, with the build it replaces
	// serving the rest throughout, on one of three schedules. The score picks it
	// only where every target the service runs on serves a share; where a target
	// declared as serving one refuses the shift, the deployer performs the row
	// without a control on that deploy and writes so, and a rollout that ran no
	// comparison is on the record as one.
	StrategyWithControl Strategy = "with_control"
)

// Strategies is every strategy a production deploy may name. The CHECK
// constraint in [DDL] lists the same two, and TestDDLListsEveryValue fails if
// the two stop agreeing.
var Strategies = []Strategy{StrategyWithoutControl, StrategyWithControl}

// Status is where the deploy as a whole is. It advances in place: the deploy
// record is an ordinary record and not a row of the log, and nothing chains it,
// so a status is an update where a gate's verdict is a second row.
type Status string

const (
	// StatusStarted is how every deploy record is written, and what a partly
	// reached deploy stays until every target is complete. A reader tells it
	// from a stopped deployer by reading the targets.
	StatusStarted Status = "started"
	// StatusComplete is every target of the deploy marked complete.
	StatusComplete Status = "complete"
	// StatusFailed is the deployer having stopped the deploy before any target
	// was complete, naming the step that stopped it. That is where a failure
	// stands for Ops, what the restart leaves alone, and what the two queries
	// over overlapping windows descend past. Every reader of what is running
	// reads a target marked complete, so a failed record moves no reader.
	StatusFailed Status = "failed"
)

// Statuses is every status a deploy may have. The CHECK constraint in [DDL]
// lists the same three, and TestDDLListsEveryValue fails if the two stop
// agreeing.
var Statuses = []Status{StatusStarted, StatusComplete, StatusFailed}

// The steps a deploy is stopped at, named on the record beside [StatusFailed].
// The column is text and these are the ones this package writes: what a reader
// needs is the step in words, and a closed set here would have to grow with
// every step the deployer gains.
const (
	// StepSnapshot is the copy before a destructive schema change that could
	// not be taken and verified. It pages.
	StepSnapshot = "taking and verifying the snapshot before a change that destroys stored data"
	// StepSchemaChange is a change that failed to apply. No traffic shifted and
	// the previous release stays current.
	StepSchemaChange = "applying the build's schema change to the service's store"
	// StepArtifactDigest is the slow rollback's verification: the artifact the
	// build names no longer digests to what the build recorded. It pages.
	StepArtifactDigest = "verifying the artifact's digest before redeploying it"
	// StepFirstTarget is the first target of the deploy refusing what it was
	// asked, with none complete behind it.
	StepFirstTarget = "reaching the first target of the environment"
	// StepStopped is what the restart writes over a deploy no target finished:
	// the deployer stopped, and nothing about the deploy advanced.
	StepStopped = "the deployer stopped before any target was complete"
)

// Completion is what the record holds per target. It is a field of the deploy
// record spread over rows and not a status of its own.
type Completion string

const (
	// CompletionNotReached is a target the deploy has not put the build on yet,
	// which is how every target of a deploy starts. A deploy that reached three
	// targets of four is a recorded partial deploy and not a mismatch found
	// after the fact.
	CompletionNotReached Completion = "not_reached"
	// CompletionComplete is a target running what the record names. Every reader
	// of what is running reads this.
	CompletionComplete Completion = "complete"
	// CompletionRolledBack is a target the rollback that undid this deploy has
	// completed on, written target by target as it does, so a rollback that
	// stopped undoes nothing on the record.
	CompletionRolledBack Completion = "rolled_back"
)

// Completions is every value a target may hold. The CHECK constraint in [DDL]
// lists the same three.
var Completions = []Completion{CompletionNotReached, CompletionComplete, CompletionRolledBack}

// Deploy is one deploy record as it is stored. At is when the record was
// written, which is when the deploy started and when the window opens over it;
// the record advances in place, and what each target did and when is on
// [Target].
type Deploy struct {
	ID            string
	Actor         record.Actor
	At            string
	ServiceID     string
	EnvironmentID string
	// Number is the sequence the deployer assigned per service and environment
	// as it began this deploy, so two deploys of one release onto one
	// environment are two records. A reader of the current release orders by
	// the release number and never by this.
	Number int64
	// BuildID is the build the deploy put on the targets. It is empty on a
	// removal, which puts nothing there.
	BuildID string
	// ReleaseID is the release the deploy is of, and is empty on three: a deploy
	// into a candidate's own environment, one gate before the number exists; a
	// deploy the search calls for, there being no release for a build of a
	// commit on no branch; and a removal, which names none at all.
	ReleaseID string
	// DeliveredReleaseIDs is a revert's deploy listing what it delivers: the
	// releases the hold was holding, whose code is in its build.
	DeliveredReleaseIDs []string
	// StrategyPicked is what the score picked and StrategyPerformed what the
	// deployer performed. Both are empty on a deploy that is not into
	// production. The performed one is what the window, Ops, and the score when
	// it learns read.
	StrategyPicked    Strategy
	StrategyPerformed Strategy
	Status            Status
	// FailedStep is the step the deployer stopped at, on a failed record and
	// nowhere else.
	FailedStep string
	// SchemaChange is the change this deploy's build carries, and empty where it
	// carries none. Which changes the store carries is read from the store's own
	// history; this is what this deploy did.
	SchemaChange string
	// SchemaChangeCompleted is whether that change completed.
	SchemaChangeCompleted bool
	// Snapshot is the copy taken and verified before a change that destroys
	// stored data, and the deletion written when it is deleted.
	Snapshot Snapshot
	// ConfigurationDigest is over the resolved value set the build runs under.
	// The configuration version so named is restored with the release at a
	// rollback.
	ConfigurationDigest string
	// WayInTokenDigest is the digest of the token minted for the way in at this
	// deploy, never the token. Nothing reads it: the way in and the report store
	// are not built.
	WayInTokenDigest string
	// ControlTarget is the target a control ran on, and is empty on every deploy
	// without one.
	ControlTarget string
	// Undoing is what a rollback's own deploy record names beside the deploy it
	// is, and is empty on every other record. [Undoing.Any] is what tells the
	// two apart.
	Undoing Undoing
}

// Snapshot is the copy of the service's store a deploy took before a change
// that destroys stored data, and what became of it. A record with no name took
// none.
type Snapshot struct {
	Name   string
	Digest string
	// DeletedAt is when the deployer deleted the copy, at the end of the
	// service's snapshot retention or earlier at an owner's call from Ops. After
	// it, the record names a copy that is gone.
	DeletedAt string
}

// Target is one target's row of the deploy record: whether it has the release
// yet, what the deploy did there, and the kept fleet's span.
type Target struct {
	DeployID string
	// Position is the target's place in the environment's order, which is the
	// order the deployer reaches them in.
	Position   int
	Address    string
	Completion Completion
	// KeptInstances is how many instances of the release a rollback of this one
	// would return to the deployer is keeping here: the capacity that release
	// had, times the fraction its owner authored. It is written when the deploy
	// starts, which is when the window opens over it.
	KeptInstances int
	// Replacement is what the seam reported when it replaced the instances here:
	// a drain, or a cut where the platform could not hold a request open across
	// the replacement.
	Replacement targetseam.Replacement
	// ReachedAt is written before the deployer calls this target and CompleteAt
	// after, both carrying the fencing token.
	ReachedAt  string
	CompleteAt string
	// ReplacedAt is when the kept fleet here was torn down, which closes the
	// span the instance-hours are summed over.
	ReplacedAt string
	// InstanceHours is the kept instances times the hours between CompleteAt and
	// ReplacedAt, and Priced is what that converted to at the rate in force at
	// the write, fixed there and never repriced.
	InstanceHours float64
	Priced        Priced
}

// Priced is an amount in the currency the owner's rates are authored in, and
// the rate it was converted at. InForce is false where the service record
// carried no instance-hour rate, which is not an amount of zero.
type Priced struct {
	Amount  float64
	Rate    float64
	InForce bool
}

// Undoing is what a rollback names beside being a deploy of the release it
// returns to: the release it failed, the releases it skipped, and the source
// that called for it.
//
// The failed release is a field apart from the skipped ones because the hold and
// the one-revert-per-failed-release rule both need the two apart: one failed
// release is one revert item, and the skipped ones were never failed — their
// code is still on master and the revert redelivers them.
//
// The source is beside the actor rather than instead of it. The actor stays the
// deployer that performed the rollback, and the source is what called for it:
// [SourceHealthMonitorAtFailed], [SourceOfHuman] for the named human at Ops with
// the reason they state, or [SourceSearch] for a deploy the search called for.
//
// There is no revert intent here. A revert is an ordinary item decomposition
// writes, from an intent intake wrote at the rollback, and the release it
// reverts is reachable through that intent's evidence — a column here would be a
// second edge pointing the other way and able to disagree with it.
type Undoing struct {
	FailedReleaseID   string
	SkippedReleaseIDs []string
	Source            string
}

// Any reports whether the record is a rollback's. A rollback always names the
// release it failed and a source; nothing else names either.
func (u Undoing) Any() bool { return u.FailedReleaseID != "" }

// SourceHealthMonitorAtFailed is the source of every rollback the factory
// performs on its own: the comparison having crossed the boundary against the
// release inside its analysis window. A rollback from this source is reported
// and not requested, and reporting is not paging.
const SourceHealthMonitorAtFailed = "the health monitor at the analysis window's failed exit"

// SourceSearch is the source of a rollback the search's own deploy returns
// from: the search deploys one build at a time and traffic returns to the
// instances of the rollback's target, which the search never tears down.
const SourceSearch = "the search, at the end of one of its windows"

// SourceOfHuman is the source of a rollback a human called for from Ops, which is
// the first phase of undoing a change after it shipped. The reason is required
// because a human's judgment about live software is the whole of the evidence
// for one.
func SourceOfHuman(name, reason string) string {
	return "the human " + name + " at Ops: " + reason
}

// What a deploy names as the thing deployed: the build it put there, and the
// release it is a deploy of where there is one. [OfRelease], [OfBuild] and
// [OfRemoval] are what a caller says it with — the fields are two ids of the
// same shape, and a caller passing them positionally could swap them and
// compile.
//
// The build is on a deploy that put one there because the build is what runs: a
// release is the name a build has on master, which is a fact of this store and
// not of the target, so a target reports the build it is running and the build
// is what makes what runs comparable to what the record says.
type What struct {
	ReleaseID string
	BuildID   string
}

// OfRelease is a deploy of a numbered release, which is every deploy onto an
// environment fed from master but the search's and the removal's. It names the
// release's own build, because that is what goes on the target.
func OfRelease(releaseID, buildID string) What {
	return What{ReleaseID: releaseID, BuildID: buildID}
}

// OfBuild is a deploy of a build under no release: a candidate's build onto the
// candidate's own environment, where there is no number yet, and a build the
// search called for, made from commits master keeps and on no branch.
func OfBuild(buildID string) What { return What{BuildID: buildID} }

// OfRemoval is a deploy that puts nothing anywhere: the service is taken off
// the environment's targets. It names neither a release nor a build, and the
// service's current release is none once it is complete on every target.
func OfRemoval() What { return What{} }

// Removal reports whether the deploy put nothing there, which is what clears
// the current release.
func (w What) Removal() bool { return w.BuildID == "" && w.ReleaseID == "" }

// AdvisoryLockKey is the PostgreSQL advisory lock [Writer.Start] takes for the
// whole of its transaction, one key per service and environment: the first
// eight bytes of SHA-256 of [lockName] plus the two ids, big-endian, with the
// top bit passed so the value is positive. TestAdvisoryLockKeyIsDerivedFromTheName
// recomputes it. Per pair rather than one key, because two pairs' sequence
// numbers have nothing to serialise against each other for.
func AdvisoryLockKey(serviceID, environmentID string) int64 {
	sum := sha256.Sum256([]byte(lockName + serviceID + "/" + environmentID))
	return int64(binary.BigEndian.Uint64(sum[:8]) & 0x7fffffffffffffff)
}
