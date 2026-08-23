package score

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
)

// SampleRate is the share of firings the score would have gated that it holds
// out of the gate instead. It is the score's own number and not an owner's: gate
// policy is seven rows and the sample is not one of them, and an owner who wants
// a row never sampled adds a safeguard at it, which the sample may not pass.
//
// One in ten is where it starts. Lower and the unbiased evidence the threshold's
// rise depends on arrives too slowly to move anything on an install shipping a
// few items a day; higher and the factory is auto-passing changes it wanted gated
// often enough that an owner would notice it as the score having changed its mind.
const SampleRate = 0.10

// Draw is the random number the sample is drawn from. It is an interface because
// the selection has to be repeatable in a test and random in a factory, and
// because a package that reached a global source of randomness could not be asked
// twice for the same answer.
type Draw interface {
	// Fraction is a number in [0,1), drawn once per firing the sample may select
	// at.
	Fraction() float64
}

// RandomDraw is the draw a factory runs with. It is what [New] composes where a
// caller hands none.
type RandomDraw struct{}

// Fraction is a number in [0,1) from the runtime's own generator.
func (RandomDraw) Fraction() float64 { return rand.Float64() }

// NeverDraw selects nothing. It is what a caller composes to run with no sample
// at all — which is a factory whose threshold can fall and never rise, because
// the only unbiased evidence for a rise is the sample's.
type NeverDraw struct{}

// Fraction is one, which is above every rate.
func (NeverDraw) Fraction() float64 { return 1 }

// Selection is what the sample decided about one firing: whether the score is
// holding this item out, and which of the two ways it came to be held out. The
// reason is on the opening row because a firing that reads the same to an owner
// for two different reasons is one they cannot argue with.
type Selection struct {
	HeldOut bool
	Why     string
}

// The two ways an item is held out, in the words the opening row stores.
const (
	// SelectedHere is the draw having selected this item at this firing.
	SelectedHere = "the score's sample selected this item at this firing"
	// SelectedEarlier is an earlier decision on this item having said the score
	// selected it. An item selected once auto-passes every gate the score would
	// have gated, so the selection is read forward from where it was made.
	SelectedEarlier = "the score's sample selected this item at an earlier gate"
)

// The two things an auto-pass comes from, in the words the closing row stores.
// They are this package's and not the gate's because the field they are written
// into is one this package reads back: the threshold's calibration counts what the
// score auto-passed on the number apart from what its own sample auto-passed, and
// a second spelling of either word here would be two able to disagree.
const (
	// AutoPassThreshold is the number having been under the threshold in
	// force. It is what a firing the score would have passed anyway reads, whether
	// or not the item is held out.
	AutoPassThreshold = "threshold"
	// AutoPassSample is the number having been at or above the threshold with
	// the item held out. This is the value the threshold's rise is counted over,
	// and it is the whole reason the sample exists.
	AutoPassSample = "the score's held-out sample"
)

// HoldOut is whether the score holds this item out of the gate the firing would
// otherwise put a human at. It is asked after the policy has been read, because
// the question is about a gate the score itself would have gated and the score
// does not know the threshold in force.
//
// Three rules, in this order. A safeguard is never passed: a human a safeguard
// added at a gate is a human an owner added, and a sample that could pass one
// would be the single mechanism in the design that removes a human from a gate,
// which is what a safeguard exists to prevent. An item an earlier decision says
// was selected stays selected, whatever the number reads now — that is what makes
// the selection an item's property rather than a firing's. And otherwise the draw
// selects, but only where the score would have gated: holding out what it was
// going to pass anyway would produce no evidence about a gate.
//
// A firing over a set is not sampled. Decomposition decides over several items at
// once, so one draw there would select several items on one number, and the row's
// number is its riskiest member's rather than any one item's. What that costs is
// that decomposition's own row never produces unbiased evidence, and the threshold an
// owner authors there moves only by falling.
func (s *Score) HoldOut(ctx context.Context, itemID string, wouldGate, bySafeguard bool) (Selection, error) {
	if itemID == "" || bySafeguard {
		return Selection{}, nil
	}
	selected, err := heldOutBefore(ctx, s.pool, itemID)
	if err != nil {
		return Selection{}, err
	}
	if selected {
		return Selection{HeldOut: true, Why: SelectedEarlier}, nil
	}
	if !wouldGate {
		return Selection{}, nil
	}
	if s.draw.Fraction() < SampleRate {
		return Selection{HeldOut: true, Why: SelectedHere}, nil
	}
	return Selection{}, nil
}

// heldOutBefore is whether any decision already opened on this item says the
// score selected it. The selection is recorded on the decisions and nowhere else —
// the design's own arrangement, so that a reader of one decision can see it —
// which makes the log the place the stickiness is read from.
//
// Opening rows are read whether or not their decision has closed: an item selected
// at a firing that has not been decided yet is still selected, and the next row to
// fire has to know.
func heldOutBefore(ctx context.Context, pool *pgxpool.Pool, itemID string) (bool, error) {
	rows, err := decisionlog.Read(ctx, pool)
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		if row.Shape != decisionlog.ShapeDecision || row.Part != decisionlog.PartOpening {
			continue
		}
		var opening Opening
		if json.Unmarshal([]byte(row.Payload), &opening) != nil {
			continue
		}
		if opening.ItemID == itemID && opening.HeldOut {
			return true, nil
		}
	}
	return false, nil
}

// HeldOutItems is every item any decision says the score selected, in the order
// the selections were made. It is what the crude interface prints and what a
// reader asking which items reached production with a human removed follows.
func HeldOutItems(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := decisionlog.Read(ctx, pool)
	if err != nil {
		return nil, err
	}
	var items []string
	seen := map[string]bool{}
	for _, row := range rows {
		if row.Shape != decisionlog.ShapeDecision || row.Part != decisionlog.PartOpening {
			continue
		}
		var opening Opening
		if err := json.Unmarshal([]byte(row.Payload), &opening); err != nil {
			continue
		}
		if opening.HeldOut && !seen[opening.ItemID] {
			seen[opening.ItemID] = true
			items = append(items, opening.ItemID)
		}
	}
	return items, nil
}

// HeldOut is whether the score selected one item, read off the decisions. It is
// what the health monitor asks before it opens a window: a held-out release runs to
// the cap rather than stopping where the boundary would allow, so the exit it may
// take is a fact of the open and is copied onto the record there.
func HeldOut(ctx context.Context, pool *pgxpool.Pool, itemID string) (bool, error) {
	if itemID == "" {
		return false, fmt.Errorf("%w: the sample selects an item, and none is named", ErrChangeIncomplete)
	}
	return heldOutBefore(ctx, pool, itemID)
}
