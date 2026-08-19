package item

import "github.com/dulguun0225/borg/factory/record"

// Stage is where an item is in the pipeline. M1's path touches three, and the
// design's other values arrive with the milestones that write them.
type Stage string

const (
	// StageSpec is where [Cut.Create] writes every item.
	StageSpec Stage = "spec"
	// StageImplementation follows spec.
	StageImplementation Stage = "implementation"
	// StageMerged follows implementation, and M1's path ends there.
	StageMerged Stage = "merged"
)

// StageOrder is every stage an item may be at, in the order [Dispatch.Advance]
// advances them. The CHECK constraints in [DDL] list the same three, and
// TestDDLListsEveryStage fails if the lists stop agreeing.
var StageOrder = []Stage{StageSpec, StageImplementation, StageMerged}

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
