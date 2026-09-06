package score

import (
	"fmt"
	"math"
	"time"

	"github.com/dulguun0225/borg/factory/gatepolicy"
)

// windowParameters is the analysis window's parameters per service: the size and
// the power per quantity, and the cap. The confidence is not among them and that
// is stated rather than left to be discovered — no outcome says the confidence a
// comparison must reach was too high, and the one thing that establishes a failed
// release was fine is a human marking the rollback as not caused by the release,
// which says the comparison was confounded and nothing about how confident it
// should have been.
//
// Which parameter an event moves is a fact of the record rather than a judgment
// about the regression. A miss on a window that closed passed cleared what it
// should have caught, so it is evidence about the power; a miss on a window that
// timed out ruled nothing out at all, so it is evidence about the size. Reading
// one event as both would move two supplied values against a single piece of
// evidence.
//
// The size is the coarser of two numbers, and that is the whole of the answer to
// the ratchet. What the evidence asks for gets finer per miss and never coarser.
// The finest size the traffic reaches is the window's own arithmetic, computed
// there and reported on the record — a size finer than that is a size the
// comparison can never rule anything out at, so the window ends at the cap every
// time, protects nothing, and holds the next deploy for the whole cap.
//
// The cap tracks how long that service's windows actually take to resolve, in
// both directions: a cap under what a window needed closes unresolved one that
// would have resolved, and a cap far above it holds the next deploy for nothing.
func windowParameters(e *Evidence) ([]Supplied, error) {
	size, _ := Starting(gatepolicy.WindowSize)
	power, _ := Starting(gatepolicy.WindowPower)
	startingCap, _ := Starting(gatepolicy.WindowCap)

	var moved []Supplied
	for _, service := range e.Services() {
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

		sized, sizeInForce := sizes(e, service, size.Value)
		moved = append(moved, sized...)
		moved = append(moved, powers(e, service, power.Value, sizeInForce)...)
	}
	return moved, nil
}

// sizes is the size in force per quantity on one service: the coarser of what the
// evidence asks for and the finest size the traffic reached, the latter read off
// the freshest closed window that reports one. It answers what moved and the size
// in force on every quantity, moved or not, which is what the power's own rule is
// read against in the same pass.
//
// Which quantity's size a miss names is the incident's, and where the incident
// names none — a human's undo among them — the miss moves the size of every
// quantity, which is what the design says of an undo.
func sizes(e *Evidence, service string, startingSize float64) ([]Supplied, map[gatepolicy.Quantity]float64) {
	misses := map[gatepolicy.Quantity]int{}
	for _, m := range e.missesOnATimedOutWindow(service) {
		for _, quantity := range m.quantities {
			misses[quantity]++
		}
	}
	reached, known := e.finestSizeReached(service)

	var moved []Supplied
	inForce := map[gatepolicy.Quantity]float64{}
	for _, quantity := range gatepolicy.Quantities {
		value := math.Max(windowSizeFloor, startingSize/math.Pow(2, float64(misses[quantity])))
		why := ""
		if misses[quantity] > 0 {
			why = fmt.Sprintf("%d window(s) on this service timed out over a release an incident naming this quantity was raised against, so nothing was ruled out at the size they watched at",
				misses[quantity])
		}
		if known && reached[quantity] > value {
			value = reached[quantity]
			why = fmt.Sprintf("%sthe newest closed window of this service reports %v as the finest size its traffic reached on this quantity inside the cap",
				prefix(why), reached[quantity])
		}
		inForce[quantity] = value
		if why == "" || value == startingSize {
			continue
		}
		moved = append(moved, Supplied{
			Parameter: gatepolicy.WindowSize, Subject: QuantitySubject(service, quantity),
			Value: value, Why: why,
		})
	}
	return moved, inForce
}

// powers is the power in force per quantity on one service. It rises on a false
// pass — a window that closed passed over a release an incident was later raised
// against, the exit that rules a regression out having cleared one it should have
// caught — and falls where the service's windows run to the cap on volume a lower
// rate would have closed within, which is the power's own observable and not the
// size's.
func powers(e *Evidence, service string, startingPower float64, sizeInForce map[gatepolicy.Quantity]float64) []Supplied {
	falsePasses := len(e.falsePasses(service))

	var moved []Supplied
	for _, quantity := range gatepolicy.Quantities {
		// The run is read per quantity against the size in force for that
		// quantity, which the pass has just supplied: a window that reached the
		// size on one quantity and not on another is volume a lower power would
		// have closed passed within on the first alone.
		timedOut := e.timedOutRun(service, quantity, sizeInForce[quantity])

		value, why := startingPower, ""
		switch {
		case falsePasses > 0:
			value = math.Min(windowPowerCeiling, 1-(1-startingPower)/math.Pow(2, float64(falsePasses)))
			why = fmt.Sprintf("%d window(s) on this service closed passed over a release an incident was raised against, so a regression of the size in force reached passed",
				falsePasses)
		case timedOut >= windowsPerPowerFall:
			value = math.Max(windowPowerFloor, startingPower-windowPowerStep)
			why = fmt.Sprintf("%d window(s) of this service in a row timed out on traffic that reached the size in force on this quantity, which is volume a lower power would have closed passed within",
				timedOut)
		}
		if why == "" || value == startingPower {
			continue
		}
		moved = append(moved, Supplied{
			Parameter: gatepolicy.WindowPower, Subject: QuantitySubject(service, quantity),
			Value: value, Why: why,
		})
	}
	return moved
}

// prefix joins the two halves of a size's reason where both apply, so a row that
// was tightened by a miss and then held back by the traffic says both.
func prefix(why string) string {
	if why == "" {
		return ""
	}
	return why + "; and "
}

// windowLimits is the window limit per service, folded over that service's own
// history in order. The fold and not a count, because the two kinds of evidence
// are not commutative: three windows closing passed after a rollback are a
// service earning its throughput back, and the same three before it are a service
// that had it and lost it.
//
// What moves it is stated exit by exit. A window closing passed raises it and a
// rollback that undid more than its target lowers it; timed out and skipped raise
// nothing, a release nothing ruled anything out on not being a release that
// behaved. A rollback a human marked is excluded from both.
func windowLimits(e *Evidence) []Supplied {
	start, _ := Starting(gatepolicy.WindowLimit)
	var moved []Supplied
	for _, service := range e.Services() {
		limit := start.Value
		passed, rollbacks := 0, 0
		since := 0
		for _, event := range e.serviceHistory(service) {
			switch {
			case event.passed:
				passed++
				since++
				if since >= windowsPerRaise {
					since = 0
					limit = math.Min(windowLimitCeiling, limit+1)
				}
			case event.undidMoreThanItsTarget:
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
			Why: fmt.Sprintf("%d window(s) of this service closed passed and %d rollback(s) undid more than their target, folded in order", passed, rollbacks),
		})
	}
	return moved
}
