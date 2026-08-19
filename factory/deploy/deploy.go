package deploy

import "github.com/dulguun0225/borg/factory/record"

// Strategy is how a release takes live traffic from the build it replaces.
type Strategy string

const (
	// StrategyStraight is all of the traffic, in place, with none of the
	// build it replaces left running. It is the one strategy M1 writes: with
	// a control is M4's row, and widening the CHECK in [DDL] is that
	// milestone's edit — a CHECK is a schema edit each time a value is added,
	// which is what listing the values in the store costs.
	StrategyStraight Strategy = "straight"
)

// Strategies is every strategy a deploy may have. The CHECK constraint in
// [DDL] lists the same one, and TestDDLListsEveryStrategyAndStatus fails if
// the two stop agreeing.
var Strategies = []Strategy{StrategyStraight}

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
	// StatusRolledBack is written by nothing until M4 builds rollback; the
	// value is in the CHECK because the record's definition names it.
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
