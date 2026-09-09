package score

import (
	"math"

	"github.com/dulguun0225/borg/factory/window"
)

// fitEvidence is how many held-out decisions of a set must have resolved windows
// before that set's weights are fitted at all. A set whose held-out decisions are
// too few to fit keeps the weights the product shipped for it, and the counts on
// its bands say so.
const fitEvidence = 10

// Scale is the last step of one set's fit: what carries the number the weighted
// means reduce to onto the one scale every set shares, where the number
// estimates the share of held-out windows that failed among decisions taken at
// that number on that set.
//
// The weights alone cannot do it. Each term of the published formula is a
// weighted mean, so multiplying a term's weights through changes no number, and
// what a fit of the weights decides is which factors rank a change against
// another and never where the ranking sits against a failure share. So the
// shares of separation are the shape and this is the scale, and both are
// fitted by the same recalibration over the same held-out decisions.
//
// The zero value is the identity: a set no recalibration has fitted returns the
// weighted means' own number, which is where the product's own calibration put
// it.
type Scale struct {
	Intercept float64 `json:"intercept"`
	Slope     float64 `json:"slope"`
}

// Fitted reports whether this scale came out of a fit. A slope of nothing or
// below is a number that did not rank the held-out releases at all, which the
// bands report and the scale does not paper over.
func (s Scale) Fitted() bool { return s.Slope > 0 }

// Apply carries one raw number onto the fitted scale, held to the range every
// factor level shares. An unfitted scale returns the raw number.
func (s Scale) Apply(raw float64) float64 {
	if !s.Fitted() {
		return raw
	}
	return math.Min(1, math.Max(0, s.Intercept+s.Slope*raw))
}

// Fit refits each factor set's weights on the held-out decisions taken on that
// set alone, to the one scale every set shares: within each term of the formula
// a factor's weight is its own separation — how far its mean level on held-out
// releases whose windows failed sits from its mean level on ones whose windows
// passed — as a share of the separations of that term's factors. A factor that
// separates nothing gets nothing, and a term whose factors all separate nothing
// keeps what the product shipped. A factor in [neverWeighed] keeps what the
// product shipped whatever it separates, which is the one thing here that is
// not a reading of the outcomes.
//
// It is fitted per set because one threshold is read against three sets, so the
// number every set returns has to be on one scale or the threshold would mean
// three things.
func Fit(e *Evidence) map[FactorSet]Weights {
	fitted := map[FactorSet]Weights{}
	for _, set := range FactorSets {
		fitted[set] = fitSet(e, set)
	}
	return fitted
}

// neverWeighed is every factor a refit leaves at the weight the product shipped
// however well it separates. There is one.
//
// context.harm_marked_report is it. The design says the harm mark adds no gate a
// report did not already meet: the row was a human's already, on the source of
// the same intent, and what the mark adds is that the human deciding sees which
// report is marked. A weight above nothing would be a gate it added — a marked
// report would raise the number, and the number is what decides whether a human
// is asked at all — so the fit skips it and the shipped nothing stands. It
// separates as well as any factor and that is the point: separation is what the
// fit pays a factor for, and this is the one the design forbids paying.
//
// context.protection_withdrawn is not here though it ships at nothing too. Its
// own comment in factorsets.go says a recalibration fits it from the held-out
// decisions like every other, so what it ships at is a starting value and not a
// bound.
//
// What it costs is that the fit cannot learn what the mark predicts. That
// reading is not lost — the level is on every vector, and calibration's drift
// readings are over the same population — but nothing here turns it into a
// weight, and turning it into one would have to be a change to the design
// first.
var neverWeighed = map[string]bool{contextHarmMarkedReport.name: true}

// separation is one factor's mean level on the two kinds of held-out release.
type separation struct {
	failed []float64
	passed []float64
}

func fitSet(e *Evidence, set FactorSet) Weights {
	readings := map[string]*separation{}
	resolved := 0
	for _, f := range e.firings {
		if !f.OpenEvent.HeldOut || f.OpenEvent.FactorSet != set {
			continue
		}
		r, released := e.releaseOfItem[f.OpenEvent.ItemID]
		if !released || e.marked[r.ID] {
			continue
		}
		w, watched := e.windowOfRelease[r.ID]
		if !watched || (w.Exit != window.ExitPassed && w.Exit != window.ExitFailed) {
			continue
		}
		resolved++
		for _, v := range f.OpenEvent.Vector {
			if v.Resolved != "" {
				continue
			}
			if readings[v.Name] == nil {
				readings[v.Name] = &separation{}
			}
			if w.Exit == window.ExitFailed {
				readings[v.Name].failed = append(readings[v.Name].failed, v.Level)
			} else {
				readings[v.Name].passed = append(readings[v.Name].passed, v.Level)
			}
		}
	}
	if resolved < fitEvidence {
		return ShippedWeights(set)
	}

	weights := Weights{}
	for _, term := range []Term{TermLikelihood, TermImpact, TermReversibility} {
		names := factorsOf(set, term)
		total := 0.0
		separations := map[string]float64{}
		for _, name := range names {
			// A factor the fit never weighs takes no share and adds nothing to
			// the total the shares are taken out of, so the term's other
			// factors are fitted as though it were not there.
			if neverWeighed[name] {
				continue
			}
			r := readings[name]
			if r == nil || len(r.failed) == 0 || len(r.passed) == 0 {
				continue
			}
			separations[name] = math.Abs(mean(r.failed) - mean(r.passed))
			total += separations[name]
		}
		if total == 0 {
			for _, name := range names {
				weights[name] = ShippedWeights(set).Of(name)
			}
			continue
		}
		for _, name := range names {
			if neverWeighed[name] {
				weights[name] = ShippedWeights(set).Of(name)
				continue
			}
			weights[name] = separations[name] / total
		}
	}
	return weights
}

// FitScale fits each set's scale over the held-out decisions taken on that set:
// the least-squares line from the number the fitted weights give a recorded
// vector to whether that release's window failed, which is what makes the
// number an estimate of the share of held-out windows that failed at it.
//
// The number it fits from is recomputed under the weights handed in and not the
// number the decision recorded: the decision was taken under the weights in
// force then, and a scale fitted onto those would be a scale for a formula this
// recalibration is replacing.
//
// A set with too few held-out decisions, or one whose numbers all read the
// same, keeps the identity: there is nothing to fit a line through, and a scale
// invented over one point would move every number in the factory.
func FitScale(e *Evidence, weights map[FactorSet]Weights) map[FactorSet]Scale {
	fitted := map[FactorSet]Scale{}
	for _, set := range FactorSets {
		fitted[set] = fitScaleOf(e, set, weights[set])
	}
	return fitted
}

func fitScaleOf(e *Evidence, set FactorSet, weights Weights) Scale {
	var numbers, failed []float64
	for _, f := range e.firings {
		if !f.OpenEvent.HeldOut || f.OpenEvent.FactorSet != set {
			continue
		}
		r, released := e.releaseOfItem[f.OpenEvent.ItemID]
		if !released || e.marked[r.ID] {
			continue
		}
		w, watched := e.windowOfRelease[r.ID]
		if !watched || (w.Exit != window.ExitPassed && w.Exit != window.ExitFailed) {
			continue
		}
		vector := append([]Factor{}, f.OpenEvent.Vector...)
		for i := range vector {
			vector[i].Weight = weights.Of(vector[i].Name)
		}
		_, _, _, number := reduce(vector, Scale{})
		numbers = append(numbers, number)
		if w.Exit == window.ExitFailed {
			failed = append(failed, 1)
		} else {
			failed = append(failed, 0)
		}
	}
	if len(numbers) < fitEvidence {
		return Scale{}
	}
	meanNumber, meanFailed := mean(numbers), mean(failed)
	covariance, variance := 0.0, 0.0
	for i, n := range numbers {
		covariance += (n - meanNumber) * (failed[i] - meanFailed)
		variance += (n - meanNumber) * (n - meanNumber)
	}
	if variance == 0 || covariance <= 0 {
		return Scale{}
	}
	slope := covariance / variance
	return Scale{Intercept: meanFailed - slope*meanNumber, Slope: slope}
}

// factorsOf is the names of one set's factors in one term of the formula.
func factorsOf(set FactorSet, term Term) []string {
	var names []string
	for _, d := range definitionsOf(set) {
		if d.term == term {
			names = append(names, d.name)
		}
	}
	return names
}
