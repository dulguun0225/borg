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
	ID          string
	Actor       record.Actor
	At          string
	ServiceID   string
	Environment string
	ReleaseID   string
	Strategy    Strategy
	Status      Status
}
