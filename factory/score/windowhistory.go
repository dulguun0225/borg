package score

import (
	"fmt"
	"sort"
	"time"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/window"
)

// serviceEvent is one thing that happened on a service, placed at the time it
// happened, which is what the window limit is folded over. A window is placed at
// the time it closed and a rollback at the time its record was written, because
// what each is evidence about is the event and not the deploy that led to it.
type serviceEvent struct {
	at string
	// passed is a window that closed passed, the one exit that rules a
	// regression out.
	passed bool
	// undidMoreThanItsTarget is a rollback that undid more than the release it
	// failed.
	undidMoreThanItsTarget bool
}

func (e *Evidence) serviceHistory(serviceID string) []serviceEvent {
	var events []serviceEvent
	for _, w := range e.windows {
		if w.ServiceID == serviceID && w.Exit == window.ExitPassed && !e.marked[w.ReleaseID] {
			events = append(events, serviceEvent{at: w.ClosedAt, passed: true})
		}
	}
	for _, d := range e.rollbacks {
		if d.ServiceID != serviceID || len(d.Undoing.SkippedReleaseIDs) == 0 {
			continue
		}
		if e.marked[d.Undoing.FailedReleaseID] {
			continue
		}
		events = append(events, serviceEvent{at: d.At, undidMoreThanItsTarget: true})
	}
	sort.Slice(events, func(i, j int) bool { return events[i].at < events[j].at })
	return events
}

// finestSizeReached is the finest size this service's traffic reached, per
// quantity, off the freshest closed window that reports one. The window computes
// it and reports it, which is what keeps the arithmetic in the one place that
// also decides the exits: a second copy here would be two able to disagree about
// the one number whose whole purpose is to match what the boundary does.
func (e *Evidence) finestSizeReached(serviceID string) (map[gatepolicy.Quantity]float64, bool) {
	for i := len(e.windows) - 1; i >= 0; i-- {
		w := e.windows[i]
		if w.ServiceID != serviceID || len(w.FinestSizeReached) == 0 {
			continue
		}
		return w.FinestSizeReached, true
	}
	return nil, false
}

// timedOutRun is how many of this service's windows, newest first, timed out
// while the traffic reached the size in force on one quantity. That is the
// power's own observable: volume a lower rate would have closed passed within,
// read off that service's own windows.
//
// The size in force is one value per quantity, so the run is too: the window
// reports the finest size its traffic reached on each, and the comparison is
// between the two readings of the same quantity.
func (e *Evidence) timedOutRun(serviceID string, quantity gatepolicy.Quantity, sizeInForce float64) int {
	run := 0
	for i := len(e.windows) - 1; i >= 0; i-- {
		w := e.windows[i]
		if w.ServiceID != serviceID {
			continue
		}
		if w.Exit != window.ExitTimedOut {
			break
		}
		reached, found := w.FinestSizeReached[quantity]
		if !found || reached > sizeInForce {
			break
		}
		run++
	}
	return run
}

// reachedStage is how many items have reported an attempt at one stage. It is the
// evidence count the attempt limit's own rule needs: one item that got past a
// stage first time is not grounds for supplying a limit the whole factory reads.
func (e *Evidence) reachedStage(stage item.Stage) int {
	n := 0
	for _, s := range e.stages {
		if s.Stage == stage {
			n++
		}
	}
	return n
}

// resolvedIn is how long each window of one service took to close on evidence —
// passed or failed, the two exits that are a reading of the quantity rather than
// a clock running out. It is what the cap is set above: a cap under the time a
// window of this service actually needed closes unresolved a window that would
// have resolved.
func (e *Evidence) resolvedIn(serviceID string) ([]time.Duration, error) {
	var took []time.Duration
	for _, w := range e.windows {
		if w.ServiceID != serviceID || (w.Exit != window.ExitPassed && w.Exit != window.ExitFailed) {
			continue
		}
		opened, err := record.ParseTime(w.At)
		if err != nil {
			return nil, fmt.Errorf("score: reading when window %s opened: %w", w.ID, err)
		}
		closed, err := record.ParseTime(w.ClosedAt)
		if err != nil {
			return nil, fmt.Errorf("score: reading when window %s closed: %w", w.ID, err)
		}
		took = append(took, closed.Sub(opened))
	}
	return took, nil
}

// stalls is every item of one area whose attempts at a stage reached the limit
// the score supplies for that stage and which has no release: work spent and
// thrown away, which is what a decomposition too coarse shows as.
//
// It reads the limit the score itself supplies and not the limit in force, which
// is package policy's read and would make this package a reader of what an owner
// authored.
func (e *Evidence) stalls(areaID string, limit func(item.Stage) float64) []item.StageTotals {
	inArea := map[string]bool{}
	for _, it := range e.items {
		// A superseded item is left out of both this and succeededAt. It was
		// replaced by a re-decomposition rather than given up on.
		if it.AreaID == areaID && it.Stage != item.StageSuperseded {
			inArea[it.ID] = true
		}
	}
	var stalled []item.StageTotals
	for _, s := range e.stages {
		if !inArea[s.ItemID] {
			continue
		}
		if _, released := e.releaseOfItem[s.ItemID]; released {
			continue
		}
		if float64(s.Attempts) >= limit(s.Stage) {
			stalled = append(stalled, s)
		}
	}
	return stalled
}

// succeededAt is the highest attempt at which one stage produced work that got
// past it: the attempts recorded against a stage of an item that is no longer at
// that stage. Attempts accumulate and are never reset, so the number on the row
// of a stage the item has left is how many attempts that stage took.
func (e *Evidence) succeededAt(stage item.Stage) int {
	at := map[string]item.Stage{}
	for _, it := range e.items {
		at[it.ID] = it.Stage
	}
	highest := 0
	for _, s := range e.stages {
		if s.Stage != stage || at[s.ItemID] == stage || at[s.ItemID] == item.StageSuperseded {
			continue
		}
		if s.Attempts > highest {
			highest = s.Attempts
		}
	}
	return highest
}

// reachedTheAttemptLimit is whether an item's attempts at any stage reached the
// limit the score supplies, which is the fourth way a rejection resolves.
func (e *Evidence) reachedTheAttemptLimit(itemID string) bool {
	start, _ := Starting(gatepolicy.AttemptLimit)
	for _, s := range e.stages {
		if s.ItemID == itemID && float64(s.Attempts) >= start.Value {
			return true
		}
	}
	return false
}

func sorted(seen map[string]bool) []string {
	var out []string
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
