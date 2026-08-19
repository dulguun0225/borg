package item

import "github.com/dulguun0225/borg/factory/record"

// Stage is where an item is in the pipeline. The path touches four, and the
// design's other values arrive with the milestones that write them.
type Stage string

const (
	// StageSpec is where [Cut.Create] writes every item.
	StageSpec Stage = "spec"
	// StageImplementation follows spec.
	StageImplementation Stage = "implementation"
	// StageQueued follows implementation: the merge gate approved the candidate
	// and its fast-forward has not happened. It is the merge queue's membership
	// — the queue is a component with no record, so what says an item is in it
	// is this value.
	StageQueued Stage = "queued"
	// StageMerged follows queued, written by the queue at the fast-forward.
	StageMerged Stage = "merged"
)

// StageOrder is every stage an item may be at, in the order [Dispatch.Advance]
// advances them. The CHECK constraints in [DDL] list the same four, and
// TestDDLListsEveryStage fails if the lists stop agreeing.
var StageOrder = []Stage{StageSpec, StageImplementation, StageQueued, StageMerged}

// Item is one item as it is stored. The actor and the time are the cut's —
// [Dispatch] advances the stage and rewrites nothing else.
type Item struct {
	ID        string
	Actor     record.Actor
	At        string
	IntentID  string
	ServiceID string
	// AreaID is the area the cut wrote, and is empty where no declared area
	// covered the work.
	AreaID string
	Branch string
	Stage  Stage
	// WaitsOn is the items this one cannot be verified until they have shipped,
	// declared by the cut. Both deploy gates hold on each of them: the candidate
	// deploy until the dependency is live, the production deploy if it has
	// stopped being.
	WaitsOn []string
	// Priority is what an owner reorders a queue with, written through
	// [Dispatch.SetPriority] and nowhere else. A greater number goes first, and
	// it orders every queue the item waits in as an item — the queues at the
	// gates up to and including Merge to master, and the merge queue.
	Priority int
}

// StageTotals is one item's bookkeeping for one stage: the attempts it has
// taken and what it spent, in tokens, accumulated across every report and
// never reset. The actor and the time are the first report's.
type StageTotals struct {
	ID          string
	Actor       record.Actor
	At          string
	ItemID      string
	Stage       Stage
	Attempts    int
	SpendTokens int64
}
