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
