package deploy

import "github.com/dulguun0225/borg/factory/record"

// Strategy is how a release takes live traffic from the build it replaces.
type Strategy string

const (
	// StrategyWithoutControl is all of the traffic, in place, with none of the
	// build it replaces left running. It is the one strategy anything writes
	// here.
	StrategyWithoutControl Strategy = "without_control"
	// StrategyWithControl is a share of the traffic, with the build it replaces
	// serving the rest throughout. Nothing writes it: serving a share means
	// deciding what fraction of arriving traffic reaches each of two builds, and a
	// target that runs a release as a local process moves a process instead — so
	// the row is unavailable on this substrate and every deploy goes without a
	// control, which is what [gate.ErrStrategySafeguardRefused] says at the row
	// where a safeguard would set one. The value is in the CHECK because the
	// record's definition names two strategies, the arrangement [StatusRolledBack]
	// had until M4 wrote it.
	StrategyWithControl Strategy = "with_control"
)

// Strategies is every strategy a deploy may have. The CHECK constraint in
// [DDL] lists the same two, and TestDDLListsEveryStrategyAndStatus fails if
// the two stop agreeing.
var Strategies = []Strategy{StrategyWithoutControl, StrategyWithControl}

// Status is where a deploy is. It advances in place: the deploy record is an
// ordinary record and not a row of the log, and nothing chains it, so a
// status is an update where a gate's verdict is a second row.
type Status string

const (
	// StatusStarted is how every deploy record is written.
	StatusStarted Status = "started"
	// StatusComplete means the target took the release. [Current] reads only
	// completed deploys.
	StatusComplete Status = "complete"
	// StatusRolledBack is a deploy a rollback undid: the failed release's own
	// deploy, and the deploy of every release the same rollback swept. It is
	// written by [Writer.Undo], which takes the source the rollback names.
	StatusRolledBack Status = "rolled_back"
)

// Statuses is every status a deploy may have. The CHECK constraint in [DDL]
// lists the same three, and TestDDLListsEveryStrategyAndStatus fails if the
// two stop agreeing.
var Statuses = []Status{StatusStarted, StatusComplete, StatusRolledBack}

// Deploy is one deploy record as it is stored. At is when the record was
// written, which is when the deploy started; the record advances in place,
// so when it completed is not a stored fact.
type Deploy struct {
	ID            string
	Actor         record.Actor
	At            string
	ServiceID     string
	EnvironmentID string
	// BuildID is the build the deploy put on the target, on every deploy.
	BuildID string
	// ReleaseID is the release the deploy is of, and is empty on a deploy into a
	// candidate's own environment. [What] says why.
	ReleaseID string
	Strategy  Strategy
	Status    Status
	// Undoing is what a rollback's own deploy record names beside the deploy it
	// is, and is empty on every other record. [Undoing.Any] is what tells the two
	// apart.
	Undoing Undoing
}

// Undoing is what a rollback names beside being a deploy of the release it
// returns to: the release it failed, the releases it swept, the source that
// called for it, and the intent it raised.
//
// The failed release is a field apart from the swept ones because the hold and
// the one-revert-per-failed-release rule both need the two apart: one failed
// release is one revert item, and the swept ones were never failed — their
// code is still on master and the revert redelivers them.
//
// The source is beside the actor rather than instead of it. The actor stays the
// deploy agent that performed the rollback, and the source is what called for it:
// [SourceHealthMonitorAtFailed], or [SourceOfHuman] for the named human at Ops
// with the reason they state.
//
// The revert intent is the one stored link from a rollback to the item that
// undoes it. Nothing on that item says it is a revert — the design keeps it an
// ordinary item — so this is where the fact lives, on the record of the event that
// raised it, which is what "revert names what raised its intent" already says. It
// is what makes the hold computable: the hold stands until the item decomposed from this
// intent has a release running.
type Undoing struct {
	FailedReleaseID string
	SweptReleaseIDs []string
	Source          string
	// RevertIntentID is empty where the rollback raised none, which is not a
	// state the health monitor produces — it raises the intent before it calls for the
	// rollback, so a rollback with no intent is one performed by something else.
	RevertIntentID string
}

// Any reports whether the record is a rollback's. A rollback always names the
// release it failed and a source; nothing else names either.
func (u Undoing) Any() bool { return u.FailedReleaseID != "" }

// SourceHealthMonitorAtFailed is the source of every rollback the factory
// performs on its own: the comparison having crossed the boundary against the
// release inside its analysis window. A rollback from this source is reported and not requested, and
// reporting is not paging.
const SourceHealthMonitorAtFailed = "the health monitor at the analysis window's failed exit"

// SourceOfHuman is the source of a rollback a human called for from Ops, which is
// the first phase of undoing a change after it shipped. The reason is required
// because a human's judgment about live software is the whole of the evidence
// for one.
func SourceOfHuman(name, reason string) string {
	return "the human " + name + " at Ops: " + reason
}

// What a deploy names as the thing deployed: the build it put there, always, and
// the release it is a deploy of where there is one. [OfRelease] and [OfBuild] are
// what a caller says it with — the fields are two ids of the same shape, and a
// caller passing them positionally could swap them and compile.
//
// The build is on every record because the build is what runs: a release is the
// name a build has on master, which is a fact of this store and not of the target,
// so a target reports the build it is running and the build is what makes what runs
// comparable to what the record says. The release is absent on a
// candidate deploy, which happens at Deploy to candidate environment, one gate
// before the merge that mints the number — a record naming a release there would
// name one nothing had minted.
type What struct {
	ReleaseID string
	BuildID   string
}

// OfRelease is a deploy of a numbered release, which is every deploy onto an
// environment fed from master. It names the release's own build, because that is
// what goes on the target.
func OfRelease(releaseID, buildID string) What {
	return What{ReleaseID: releaseID, BuildID: buildID}
}

// OfBuild is a deploy of a candidate's build onto the candidate's own
// environment, where there is no number yet.
func OfBuild(buildID string) What { return What{BuildID: buildID} }
