package score

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/lease"
)

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
// holding this item out, which of the two ways it came to be held out, and the
// rate in force at the selection. The reason is on the open event because a
// firing that reads the same to an owner for two different reasons is one they
// cannot argue with, and the rate is beside it so that a held-out outcome can be
// weighted by the probability that selected it, per service or per author, at
// whatever scope the rate was authored.
type Selection struct {
	HeldOut bool
	Why     string
	// RateInForce is the held-out sample rate this selection was drawn against:
	// what an owner authored where they authored one and what the score supplies
	// otherwise, after every safeguard clamping it on that item's service,
	// project and area. It is the caller's read, package policy being where the
	// value in force is resolved.
	RateInForce float64
}

// The two ways an item is held out, in the words the open event stores.
const (
	// SelectedHere is the draw having selected this item at this firing.
	SelectedHere = "the score's sample selected this item at this firing"
	// SelectedEarlier is an earlier decision on this item having said the score
	// selected it. An item selected once auto-passes every gate the score would
	// have gated, so the selection is read forward from where it was made.
	SelectedEarlier = "the score's sample selected this item at an earlier gate"
)

// The two things an auto-pass comes from, in the words the close event stores.
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
// does not know the threshold or the rate in force: rate is what an owner
// authored where they authored one and what the score supplies otherwise,
// clamped by every safeguard reaching that item's service, project and area.
//
// Four rules, in this order. A safeguard is never passed: a human a safeguard
// added at a gate is a human an owner added, and a sample that could pass one
// would be the single mechanism in the design that removes a human from a gate.
// A resolved vector is never passed either, whether the score could not compute
// the factor or computed a value the design resolves on — the score is in no
// position to auto-pass a gate its own number never decided. An item an earlier
// decision says was selected stays selected, whatever the number reads now. And
// otherwise the draw selects, but only where the score would have gated: holding
// out what it was going to pass anyway would produce no evidence about a gate.
//
// A firing over a set is not sampled, which the caller says by asking for no
// selection at Decomposition: that row decides over several items at once, so
// one draw there would select several items on one number.
func (s *Score) HoldOut(ctx context.Context, itemID string, rate float64,
	wouldGate, bySafeguard bool, resolved []Resolution) (Selection, error) {

	if itemID == "" || bySafeguard || len(resolved) > 0 {
		return Selection{}, nil
	}
	selectedEarlier, err := heldOutBefore(ctx, s.pool, s.token, itemID)
	if err != nil {
		return Selection{}, err
	}
	return s.decide(rate, wouldGate, selectedEarlier), nil
}

// decide is the sample's rule over what the log already said: the stickiness
// first, then the draw. It is separate from the read so that the rule is
// testable as a rule, the way every other arithmetic in this package is.
func (s *Score) decide(rate float64, wouldGate, selectedEarlier bool) Selection {
	if selectedEarlier {
		return Selection{HeldOut: true, Why: SelectedEarlier, RateInForce: rate}
	}
	if !wouldGate || rate <= 0 {
		return Selection{}
	}
	if s.draw.Fraction() < rate {
		return Selection{HeldOut: true, Why: SelectedHere, RateInForce: rate}
	}
	return Selection{}
}

// heldOutBefore is whether any decision already opened on this item says the
// score selected it. The selection is recorded on the decisions and nowhere else —
// the design's own arrangement, so that a reader of one decision can see it —
// which makes the log the place the stickiness is read from.
//
// Open events are read whether or not their decision has closed: an item selected
// at a firing that has not been decided yet is still selected, and the next row to
// fire has to know.
func heldOutBefore(ctx context.Context, pool *pgxpool.Pool, token lease.Token, itemID string) (bool, error) {
	rows, err := decisionlog.NewReader(pool, token).Read(ctx, component)
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		if row.Shape != decisionlog.ShapeDecision || row.Part != decisionlog.PartOpen {
			continue
		}
		var opening OpenEvent
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
// the selections were made. It is what the command-line interface prints and what
// a reader asking which items reached production with a human removed follows.
func HeldOutItems(ctx context.Context, pool *pgxpool.Pool, token lease.Token) ([]string, error) {
	rows, err := decisionlog.NewReader(pool, token).Read(ctx, component)
	if err != nil {
		return nil, err
	}
	var items []string
	seen := map[string]bool{}
	for _, row := range rows {
		if row.Shape != decisionlog.ShapeDecision || row.Part != decisionlog.PartOpen {
			continue
		}
		var opening OpenEvent
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
func HeldOut(ctx context.Context, pool *pgxpool.Pool, token lease.Token, itemID string) (bool, error) {
	if itemID == "" {
		return false, fmt.Errorf("%w: the sample selects an item, and none is named", ErrChangeIncomplete)
	}
	return heldOutBefore(ctx, pool, token, itemID)
}
