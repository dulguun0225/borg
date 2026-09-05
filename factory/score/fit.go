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
// keeps what the product shipped.
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
