package score

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/window"
)

// windowParameters is the analysis window's three per service. Two of them move both
// ways.
//
// The cap tracks how long that service's windows actually take to resolve, in both
// directions: a cap under what a window needed closes unresolved one that would
// have resolved, and a cap far above it holds the next deploy for nothing. The
// safeguard's direction on this parameter is a floor, and that is an owner's bound
// rather than a direction for evidence — reading it as one is what made this rule
// one-way until 2026-08-20.
//
// The size is the coarser of two numbers, and that is the whole of the answer to
// the ratchet. What harm asks for gets finer per miss and never coarser. What the
// service's traffic can resolve is arithmetic over volume, computed fresh from the
// read its newest closed window carries — and a size finer than that is a size the
// comparison can never rule anything out at, so the window ends at the cap every
// time, protects nothing, and holds the next deploy for the whole cap. Asking for
// the finer of the two would be the factory watching longer and catching less.
//
// The two inputs are different questions and only one of them is about harm, which
// is why [_The analysis window_]'s restriction to evidence traceable to the health
// signal is not being reopened here: that restriction governs what is worth
// catching, and reachability governs whether anything can be caught at all.
//
// The confidence stays one-way, and the reason is arithmetic rather than a missing
// rule: the units a window needs grow as the log of one over one-minus-confidence
// and as the inverse square of the size, so tightening the confidence from the
// convention to its ceiling costs about a doubling where two halvings of the size
// cost sixteen times. It does not compound, so it does not produce the failure the
// size's own rule exists to prevent.
func windowParameters(e *Evidence) ([]Supplied, error) {
	size, _ := Starting(gatepolicy.WindowSize)
	confidence, _ := Starting(gatepolicy.WindowConfidence)
	startingCap, _ := Starting(gatepolicy.WindowCap)

	var moved []Supplied
	for _, service := range e.Services() {
		misses := e.Misses(service)
		falsePasses := 0
		for _, w := range misses {
			if w.Exit == window.ExitPassed {
				falsePasses++
			}
		}

		// The cap first, because what the size can reach depends on how long the
		// window is allowed to run.
		capSeconds := startingCap.Value
		took, err := e.resolvedIn(service)
		if err != nil {
			return nil, err
		}
		longest := time.Duration(0)
		for _, d := range took {
			if d > longest {
				longest = d
			}
		}
		if longest > 0 {
			capSeconds = math.Min(windowCapCeiling, math.Max(windowCapFloor, 2*longest.Seconds()))
			if capSeconds != startingCap.Value {
				how := "and a cap under what a window actually needed closes unresolved one that would have resolved"
				if capSeconds < startingCap.Value {
					how = "and a cap far above what a window needs holds the next deploy for nothing"
				}
				moved = append(moved, Supplied{
					Parameter: gatepolicy.WindowCap, Subject: service, Value: capSeconds,
					Why: fmt.Sprintf("the longest window of this service to close on evidence took %s, %s", longest, how),
				})
			}
		}

		if confidenceValue := 1 - (1-confidence.Value)/math.Pow(2, float64(falsePasses)); falsePasses > 0 {
			value := math.Min(windowConfidenceCeiling, confidenceValue)
			if value != confidence.Value {
				moved = append(moved, Supplied{
					Parameter: gatepolicy.WindowConfidence, Subject: service, Value: value,
					Why: fmt.Sprintf("%d window(s) on this service closed passed over a release an incident was raised against, so the boundary said it had ruled out what it had not", falsePasses),
				})
			}
		}

		// The size: the coarser of what harm asks for and what the traffic reaches.
		wanted := math.Max(windowSizeFloor, size.Value/math.Pow(2, float64(len(misses))))
		why := ""
		if len(misses) > 0 {
			why = fmt.Sprintf("%d window(s) on this service closed without failing a release over a release an incident was raised against, so the size they watched at was too coarse",
				len(misses))
		}
		traffic, known, err := e.traffic(service)
		if err != nil {
			return nil, err
		}
		value := wanted
		if known {
			reach, err := reachable(traffic, capSeconds, confidence.Value, size.Value)
			if err != nil {
				return nil, err
			}
			if reach > value {
				value = reach
				why = fmt.Sprintf("%s%.3g unit(s) of work a second is what this service's newest closed window read, which reaches %v and no finer inside a cap of %vs",
					prefix(why), traffic.UnitsPerSecond, reach, capSeconds)
			}
		}
		if why != "" && value != size.Value {
			moved = append(moved, Supplied{
				Parameter: gatepolicy.WindowSize, Subject: service, Value: value, Why: why,
			})
		}
	}
	return moved, nil
}

// prefix joins the two halves of a size's reason where both apply, so a row that
// was tightened by a miss and then held back by the traffic says both.
func prefix(why string) string {
	if why == "" {
		return ""
	}
	return why + "; and "
}

// reachable is the finest size this service's traffic can rule anything out at
// inside its cap, on the same lattice the tightening moves along — the starting
// size halved, and halved again, down to the floor. It is the lattice and not a
// closed form so that the two inputs to the size in force are commensurable: a
// value that has been halved twice is compared against a reachable value that
// could have been halved twice.
//
// The units a size needs come from [boundary.Boundary.UnitsToClean], which is the
// same arithmetic the health monitor reads the window against. A second copy of it
// here would be two able to disagree, and this is the one number in the factory
// whose whole point is that it matches what the boundary actually does.
func reachable(t Traffic, capSeconds, confidence, startingSize float64) (float64, error) {
	available := t.UnitsPerSecond * capSeconds
	for _, size := range sizeLattice(startingSize) {
		needed, err := boundary.Boundary{Size: size, Confidence: confidence}.UnitsToClean(t.BaselineRate)
		if err != nil {
			// At this baseline rate the ratio drifts the other way, so nothing is
			// ruled out at this size however much traffic arrives. The next size up
			// is the one to ask about.
			continue
		}
		if needed <= available {
			return size, nil
		}
	}
	// Not even the size the score starts at is reachable. The score does not ask
	// for anything coarser than that: a quiet service ending every window at the
	// cap is a state the design has and reports as weak, and coarsening past the
	// calibrated value would be the factory quietly agreeing to catch less than it
	// was installed to catch.
	return startingSize, nil
}

// sizeLattice is the sizes the tightening can produce, finest first: the starting
// size halved until it reaches the floor. It is generated by halving down rather
// than doubling up from the floor, because those are two different sets — the floor
// is not a power of two below the starting size — and a reachable value taken off
// the wrong one would not be comparable with what harm asks for.
func sizeLattice(startingSize float64) []float64 {
	var descending []float64
	for size := startingSize; size > windowSizeFloor; size /= 2 {
		descending = append(descending, size)
	}
	descending = append(descending, windowSizeFloor)
	slices.Reverse(descending)
	return descending
}

// windowLimits is the window limit per service, folded over that service's own
// history in order. The fold and not a count, because the two kinds of evidence are not commutative: three
// windows closing without failing a release after a rollback are a service earning its
// throughput back, and the same three before it are a service that had it and
// lost it.
func windowLimits(e *Evidence) []Supplied {
	start, _ := Starting(gatepolicy.WindowLimit)
	var moved []Supplied
	for _, service := range e.Services() {
		limit := start.Value
		noHarm, rollbacks := 0, 0
		since := 0
		for _, event := range e.serviceHistory(service) {
			switch {
			case event.noHarm:
				noHarm++
				since++
				if since >= windowsPerRaise {
					since = 0
					limit = math.Min(windowLimitCeiling, limit+1)
				}
			case event.sweeping:
				rollbacks++
				since = 0
				limit = math.Max(start.Value, limit-1)
			}
		}
		if limit == start.Value {
			continue
		}
		moved = append(moved, Supplied{
			Parameter: gatepolicy.WindowLimit, Subject: service, Value: limit,
			Why: fmt.Sprintf("%d window(s) of this service closed without failing a release and %d rollback(s) swept a release, folded in order", noHarm, rollbacks),
		})
	}
	return moved
}
