package item

import "github.com/dulguun0225/borg/factory/record"

// Stage is where an item is in the pipeline: the six an item moves through and
// the three values that end one.
type Stage string

const (
	// StageSpec is where [Decomposition.Create] writes every item.
	StageSpec Stage = "spec"
	// StageImplementationPlan follows spec: how the item will be built.
	StageImplementationPlan Stage = "implementation_plan"
	// StageTasks follows the implementation plan: the approved plan divided
	// into work an agent picks up.
	StageTasks Stage = "tasks"
	// StageImplementation follows tasks.
	StageImplementation Stage = "implementation"
	// StageQueued follows implementation: the Merge to master gate approved the
	// candidate and its fast-forward has not happened. It is the merge queue's
	// membership — the queue is a component with no record, so what says an
	// item is in it is this value.
	StageQueued Stage = "queued"
	// StageMerged follows queued, written by [Dispatch.End] at the
	// fast-forward.
	StageMerged Stage = "merged"
	// StageDropped is where an item stops for good without reaching a release,
	// written by [Dispatch.Drop] where a human ends work for good: Work ending
	// one that escalated and nobody took over, or the intent above it, and Ops
	// ending a revert item a mark made unnecessary.
	StageDropped Stage = "dropped"
	// StageEscalated is where an item that exceeded the attempt limit stops
	// being retried, written by [Dispatch.Escalate]. It holds its branch until
	// a human ends it, and [Dispatch.ClearEscalation] is a human taking it
	// over.
	StageEscalated Stage = "escalated"
	// StageSuperseded is where an item ends when a decomposition replaces it:
	// the Decomposition row rejected the set it was part of, and the item
	// points at whatever replaced it. It is written at one event by
	// [Decomposition.Supersede].
	StageSuperseded Stage = "superseded"
)

// StageOrder is every stage an item moves through, in the order it reaches
// them: [Dispatch.Advance] moves one step along it up to queued, and
// [Dispatch.End] writes the last. It is what [Dispatch] validates a target
// against, so none of the three ending values is here — each has a write of
// its own.
var StageOrder = []Stage{
	StageSpec, StageImplementationPlan, StageTasks, StageImplementation, StageQueued, StageMerged,
}

// AuthoringStages is the stages an agent or a human authors an artifact at,
// and so the stages an attempt is counted at: an attempt is counted when a
// stage is entered to author, and nothing authors at queued or at merged.
var AuthoringStages = []Stage{StageSpec, StageImplementationPlan, StageTasks, StageImplementation}

// EveryStage is every value the stage column may hold: the six an item moves
// through and the three that end one. The CHECK constraints in [DDL] list the
// same set in the same order, and TestDDLListsEveryStage fails if the lists
// stop agreeing. It is not called Stages, which this package already uses for
// the per-stage rows of one item.
var EveryStage = append(append([]Stage{}, StageOrder...), StageDropped, StageEscalated, StageSuperseded)

// Item is one item as it is stored. The actor and the time are decomposition's
// — [Dispatch] writes the stage and the priority and rewrites nothing else.
type Item struct {
	ID        string
	Actor     record.Actor
	At        string
	IntentID  string
	ServiceID string
	// AreaID is the area decomposition wrote, and is empty where no declared area
	// covered the work.
	AreaID string
	Branch string
	Stage  Stage
	// WaitsOn is the items this one cannot be verified until they have shipped,
	// declared by decomposition. Both deploy gates hold on each of them: the candidate
	// deploy until the dependency is live, the production deploy if it has
	// stopped being.
	WaitsOn []string
	// RequirementsAnswered is the ids of the intent's requirements this item
	// answers, written by decomposition when it creates the item. A requirement
	// one item answers alone is assigned to it whole; one the split spreads over
	// several items is assigned to none of them and each item names the derived
	// requirement of its own share instead.
	RequirementsAnswered []string
	// SupersededBy is the items of the re-decomposition that replaced this one, written by
	// [Decomposition.Supersede] and empty on every item nothing replaced. It is a list
	// because a re-decomposition may replace four items with two, and it stays unwritten
	// where a re-decomposition replaced an item with nothing — what says why then is the
	// superseded stage beside the decision that rejected the set.
	SupersededBy []string
	// Priority is what an owner reorders a queue with, written through
	// [Dispatch.SetPriority] and nowhere else. A greater number goes first, and
	// it orders every queue the item waits in as an item — the queues at the
	// gates up to and including Merge to master, and the merge queue.
	Priority int
}

// StageTotals is one item's bookkeeping for one stage: how many attempts the
// stage has taken, kept across every entry and never reset at an advance, and
// the count an escalation was last cleared at. The actor and the time are the
// first entry's.
type StageTotals struct {
	ID       string
	Actor    record.Actor
	At       string
	ItemID   string
	Stage    Stage
	Attempts int
	// ClearedAtAttempts is what [Dispatch.ClearEscalation] wrote: the count the
	// stage stood at when a human took the item over. It is zero on a stage no
	// escalation was ever cleared at.
	ClearedAtAttempts int
}

// AttemptsSinceCleared is what the attempt limit is compared against: the
// attempts the stage has taken since the last escalation was cleared on it.
// The attempts already taken stay on the row, so the clearing is a mark and
// not a reset.
func (s StageTotals) AttemptsSinceCleared() int { return s.Attempts - s.ClearedAtAttempts }
