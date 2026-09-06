package score

import (
	"fmt"
	"math"
	"sort"

	"github.com/dulguun0225/borg/factory/window"
)

// The published bounds of the two drift readings, named where they are applied.
// Every one of them is in [Rules] and TestRulesStateEveryBound holds the two
// together.
const (
	// driftReadings is how many items must have named a factor before its
	// distribution is read against its own history at all. Below it the two
	// halves are too few to say anything. The unit is the item and not the
	// decision: every row over one item weighs the one vector its build was
	// read into, so the four decisions of one item are one reading of each
	// factor and not four, and eight decisions can be two items.
	driftReadings = 8
	// driftBound is how far the newer half's mean level may sit from the older
	// half's before the factor is found drifted. A factor whose distribution has
	// moved that far is measuring something other than what the formula was
	// calibrated on.
	driftBound = 0.25
	// priorDriftReadings is how many held-out decisions of each kind — windows
	// that failed and windows that passed — the prior's own reading needs before
	// it can find one drifted.
	priorDriftReadings = 3
)

// Drift is one reading calibration made: a factor whose distribution has moved
// against its own history, or a per-author prior that no longer separates the
// held-out releases whose windows failed from the ones whose windows passed.
// Either takes the treatment an unavailable factor takes — resolved and not
// valued, a human deciding whatever the formula returns — until a recalibration
// is in force at that gate.
type Drift struct {
	// Factor is the drifted factor, and empty on a prior's own reading.
	Factor string `json:"factor,omitempty"`
	// Author is the author whose prior stands drifted, and empty on a factor's
	// reading.
	Author string `json:"author,omitempty"`
	Why    string `json:"why"`
}

// drift is every drift reading over the decisions in the store: each factor's
// distribution against its own history, and each per-author prior against what
// the windows of its held-out releases then closed.
//
// The prior is exempt from the first reading and gets the second, because its
// distribution moving is what it working looks like: it narrows as outcomes
// arrive, so read against its own history every earned narrowing would be a
// drift and the treatment would put humans at gates factory-wide for the score
// having learned something.
func (e *Evidence) drift() []Drift {
	return append(e.factorDrift(), e.priorDrift()...)
}

// factorDrift reads each factor's distribution over the decisions that named it,
// the older half against the newer. A factor whose distribution has moved is
// measuring something other than what the formula was calibrated on: an agent
// that learned what the factor rewards, an area whose shape changed, a fault in
// what supplies it.
func (e *Evidence) factorDrift() []Drift {
	levels := map[string][]float64{}
	// One reading per item per factor, taken from the item's first firing that
	// valued it: the rows over one item weigh one vector.
	read := map[string]map[string]bool{}
	ordered := append([]Firing{}, e.firings...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].At < ordered[j].At })
	for _, f := range ordered {
		for _, v := range f.OpenEvent.Vector {
			if v.Resolved != "" || v.Name == authorPrior.name {
				continue
			}
			if read[v.Name] == nil {
				read[v.Name] = map[string]bool{}
			}
			if read[v.Name][f.OpenEvent.ItemID] {
				continue
			}
			read[v.Name][f.OpenEvent.ItemID] = true
			levels[v.Name] = append(levels[v.Name], v.Level)
		}
	}

	var found []Drift
	for _, name := range sortedKeys(levels) {
		read := levels[name]
		if len(read) < driftReadings {
			continue
		}
		half := len(read) / 2
		was, now := mean(read[:half]), mean(read[half:])
		if math.Abs(now-was) < driftBound {
			continue
		}
		found = append(found, Drift{
			Factor: name,
			Why: fmt.Sprintf("over %d items naming it the mean level moved from %.2f to %.2f, past the bound of %.2f",
				len(read), was, now, driftBound),
		})
	}
	return found
}

// priorDrift reads the prior each held-out decision was taken on against what
// that release's window then closed. A prior that no longer separates the
// held-out releases whose windows failed from the ones whose windows passed is
// drifted whatever its distribution did.
//
// It is read on the held-out sample and nowhere else, for the reason the
// threshold rises on nothing else: outside the sample the prior itself decided
// which changes a human read, so the outcomes there are the ones its own
// decisions selected. A prior standing drifted stops the sample selecting on
// that author at all, so no new held-out evidence about it arrives while it
// stands, and recalibration is the only exit. Where a truncation of the log has
// removed every held-out decision on such an author, this reading finds nothing
// and the prior restarts as an unseen author's.
func (e *Evidence) priorDrift() []Drift {
	type reading struct{ failed, passed []float64 }
	byAuthor := map[string]*reading{}

	for _, f := range e.firings {
		author := f.OpenEvent.AuthorKey
		if !f.OpenEvent.HeldOut || author == "" {
			continue
		}
		level, found := priorLevel(f.OpenEvent.Vector)
		if !found {
			continue
		}
		r, released := e.releaseOfItem[f.OpenEvent.ItemID]
		if !released || e.marked[r.ID] {
			continue
		}
		w, watched := e.windowOfRelease[r.ID]
		if !watched {
			continue
		}
		if byAuthor[author] == nil {
			byAuthor[author] = &reading{}
		}
		switch w.Exit {
		case window.ExitFailed:
			byAuthor[author].failed = append(byAuthor[author].failed, level)
		case window.ExitPassed:
			byAuthor[author].passed = append(byAuthor[author].passed, level)
		}
	}

	var found []Drift
	for _, author := range sortedKeys(byAuthor) {
		r := byAuthor[author]
		if len(r.failed) < priorDriftReadings || len(r.passed) < priorDriftReadings {
			continue
		}
		onFailed, onPassed := mean(r.failed), mean(r.passed)
		if onFailed > onPassed {
			continue
		}
		found = append(found, Drift{
			Author: author,
			Why: fmt.Sprintf("over %d held-out releases whose windows failed and %d whose windows passed, the prior read %.2f on the failed and %.2f on the passed, so it no longer separates them",
				len(r.failed), len(r.passed), onFailed, onPassed),
		})
	}
	return found
}

// priorLevel is the per-author prior's level on one vector, and false where the
// vector names none — a set that weighs no prior, or a firing whose prior was
// resolved.
func priorLevel(vector []Factor) (float64, bool) {
	for _, f := range vector {
		if f.Name == authorPrior.name && f.Resolved == "" {
			return f.Level, true
		}
	}
	return 0, false
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	total := 0.0
	for _, v := range values {
		total += v
	}
	return total / float64(len(values))
}
