package gate

import (
	"context"
	"fmt"
	"math/rand/v2"

	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/score"
)

// The two samples a firing reads, which select in opposite directions. The
// held-out sample auto-passes a change the score would have gated, to keep
// unbiased signal on the authors and areas it has stopped trusting. The review
// sample puts a change the score would have auto-passed in front of a human
// anyway, so that a human's undo rate is comparable with the factory's own.

// Draw is where the review sample's randomness comes from. It is an interface so
// that a test can select deterministically, which is the arrangement the score's
// own sample already has.
type Draw interface {
	// Fraction is a number in [0,1). A row is selected where it is under the
	// rate in force.
	Fraction() float64
}

// RandomDraw is the runtime's own generator.
type RandomDraw struct{}

// Fraction is a number in [0,1) from the runtime's own generator.
func (RandomDraw) Fraction() float64 { return rand.Float64() }

// NeverDraw selects nothing. A factory composed with it runs no review sample,
// which is a factory whose gates produce no reading of a human against its own
// auto-pass rate — the difference the rate exists to remove.
type NeverDraw struct{}

// Fraction is one, which is above every rate.
func (NeverDraw) Fraction() float64 { return 1 }

// heldOut asks the score whether it holds this item out, and returns the rate in
// force at the selection beside the answer. The rate is read from the policy
// after every safeguard clamping it, so a held-out outcome can be weighted by
// the probability that selected it.
//
// The sample may pass nothing a safeguard put a human at and nothing a resolved
// vector put one at, and both facts are handed to the score rather than applied
// here: the score is the one deciding, and a gate that filtered its answer would
// be a second place the rules live.
func (g *Gate) heldOut(ctx context.Context, f Firing, s policy.Subjects, applied policy.Applied,
	overThreshold bool, resolved []score.Resolution) (score.Selection, error) {
	if f.ItemID == "" || !f.Row.ReadsAThreshold() {
		return score.Selection{}, nil
	}
	rate, err := g.policy.HeldOutSampleRate(ctx, s)
	if err != nil {
		return score.Selection{}, fmt.Errorf("gate: reading the held-out sample rate in force: %w", err)
	}
	selection, err := g.score.HoldOut(ctx, f.ItemID, rate.Number,
		overThreshold, applied.HumanBySafeguard, resolved)
	if err != nil {
		return score.Selection{}, fmt.Errorf("gate: asking the score whether %s is held out: %w", f.ItemID, err)
	}
	return selection, nil
}

// reviewSampled is whether the review sample selected this row, and the rate it
// was drawn against. Three rules, and the row is selected only where all of them
// hold.
//
// It selects among the rows the score would have auto-passed: a row the number,
// a safeguard, or a resolved vector already sent a human to is not one the
// factory judged fine, so selecting it would measure nothing. It selects no row
// on an item the held-out sample selected, that sample's evidence being a
// release that reached production with a human removed, and a review-sampled row
// would put one back at a gate the selection removed them from. And the rate is
// per duty, so a row the design names no duty for — where the rate is read for
// duty zero and nothing is authored — is drawn against what the score supplies
// for it.
func (g *Gate) reviewSampled(ctx context.Context, f Firing, s policy.Subjects, waits Waits,
	overThreshold, resolved, bySafeguard, heldOut bool) (bool, float64, error) {
	if !f.Row.ReadsAThreshold() || overThreshold || resolved || bySafeguard || heldOut {
		return false, 0, nil
	}
	s.Duty = int(waits.Duty)
	rate, err := g.policy.ReviewSampleRate(ctx, s)
	if err != nil {
		return false, 0, fmt.Errorf("gate: reading the review sample rate in force: %w", err)
	}
	return g.draw.Fraction() < rate.Number, rate.Number, nil
}
