package score

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/window"
)

// LearningVersion names the published rules by which a supplied value moves. It
// moves when a rule changes, the way [FormulaVersion] moves when the formula
// does, and a version naming it is readable against the rules that were in force.
const LearningVersion = "outcomes-1"

// Rules is the published rules, in the words the score version stores and an
// owner disagreeing with a moved value reads. A learned value nobody can argue
// with is a value nobody will trust, which is the score's own rule about its
// number applied to the seven numbers it supplies.
//
// Every rule is a function of the whole graph and never a step from the value in
// force, so two passes over one store produce one table and a pass that runs twice
// moves nothing twice. TestLearningIsIdempotent is what holds that true.
const Rules = `Each value the score supplies starts where the formula was calibrated and moves as outcomes arrive.
An outcome is read off records that already exist: a human's verdict on a decision, a watch window's
exit, a rollback and the releases it swept, an incident against a release, and the attempts a stage took.

  risk threshold      per gate row. It falls to one band (0.05) below the lowest number the score
                      auto-passed on the number at that row and whose item turned out badly, floored
                      at 0.05 — so the next change scoring what that one scored is decided by a human.
                      It rises one band per 3 held-out firings at that row whose items turned out well,
                      ceilinged at 0.90, and nothing else raises it: a gated change a human approved is
                      not evidence the gate was unnecessary, because the human's own scrutiny is part
                      of why it turned out well. That is the self-reinforcement the held-out sample
                      exists to break, and the sample is the only unbiased evidence for raising it.
                      A fall outranks a rise: it names a number the score is known to have got wrong.

  attempt bound       per stage. One above the highest attempt at which that stage produced work that
                      got past it, floored at 3 and ceilinged at 6. A retry that worked is evidence
                      the next one might; nothing lowers it, an escalation saying the bound was
                      reached and not that it was too high.

  item-size target    per area. Halved per stall in that area — an item whose attempts at a stage
                      reached the bound above and which has no release, which is work spent and thrown
                      away — floored at 50. Nothing raises it: the other end of a bad size is cost per
                      feature and rework rate, and the factory measures neither.

  watch window size   per service. Halved per miss — a window that closed clean or at the cap over a
                      release an incident was later raised against, which is the crossing the
                      comparison could have seen and did not — floored at 0.002. Each halving
                      quadruples the traffic a comparison needs, which is the inverse-square cost of
                      catching something smaller, so a service that misses often watches longer and
                      resolves less.

  window confidence   per service. Each false clean — a miss whose window closed at the clean exit
                      rather than at the cap — halves the distance from the confidence in force to
                      one, ceilinged at 0.999. A window that ended at the cap ruled nothing out, so it
                      is evidence about the size and not about the confidence.

  watch window cap    per service. Twice the longest a window of that service took to close on
                      evidence — clean or harm, the two exits that read the quantity rather than the
                      clock — floored at 86400 and ceilinged at a week. A cap under the time a window
                      of this service actually needed closes unresolved a window that would have
                      resolved; the doubling is the room a sample of one earns.

  K                   per service, and the one value that moves both ways. Folded over that service's
                      own history in order: from 1, every 3 windows closing without harm raise it by
                      one, ceilinged at 5, and a rollback that swept a release lowers it by one and
                      resets the count, floored at 1. Windows closing without harm are one-sided
                      evidence — a service that has never rolled back has seen the throughput K gives
                      and none of the rollback size it causes — which is why the rise is slow and the
                      fall is immediate.

  the predicate       nothing. No outcome teaches which kinds of assertion a declaration may draw
  catalog             from, so the score supplies none and the value in force is the kinds this
                      factory can decide, what an owner authored, and what a pin adds.

The held-out sample is how the threshold gets evidence its own decisions did not select. One firing in
ten that the score would have gated is held out instead: the item auto-passes every gate the score
would have gated from that firing onward, its closing row says the sample and not the threshold passed
it, and its release is watched to the cap rather than stopping where the boundary would allow. It
reaches nothing an owner pinned, because a gate pinned always-on is a human an owner added and no
mechanism in the tree removes one. What the sample cannot have on this substrate is the strategy that
keeps a control: every deploy here moves a process rather than traffic, so a held-out release is
watched by the same confounded comparison as every other one and the longest watch available is all
the sample gets.

Five of the six move only toward more protection, because the evidence arrives one-sided: the factory
finds out that it should have been more careful and never that it was careful enough. K is the
exception the design makes two-sided. What that costs is a ratchet — a long-lived install tightens and
nothing but an owner authoring over it loosens anything.
`

// The published rules' own numbers, named where they are applied. Every one of
// them is in [Rules] and TestRulesStateEveryBound holds the two together.
const (
	// thresholdBand is how far the threshold moves in one step, and how far below
	// a number it goes to gate what that number did not gate.
	thresholdBand = 0.05
	// thresholdFloor is as low as the threshold goes. A threshold below this puts
	// a human at nearly every gate, which is the human work the factory exists to
	// remove.
	thresholdFloor = 0.05
	// thresholdCeiling is as high as it goes. Above this the score is auto-passing
	// changes it reads as nearly the riskiest there are.
	thresholdCeiling = 0.90
	// heldOutPerBand is how many held-out firings that turned out well it takes to
	// raise the threshold one band.
	heldOutPerBand = 3
	// attemptBoundCeiling is as high as the bound goes, whatever the evidence: a
	// stage retried more than this has stopped being a stage that retries.
	attemptBoundCeiling = 6
	// itemSizeFloor is as small as the target goes. Below this an item is smaller
	// than the overhead of shipping one.
	itemSizeFloor = 50
	// windowSizeFloor is as fine as the size goes. The traffic a comparison needs
	// at this size is more than any install the design describes receives, so
	// below it the window would be watching for something it can never rule out.
	windowSizeFloor = 0.002
	// windowConfidenceCeiling is as sure as the comparison is asked to be.
	windowConfidenceCeiling = 0.999
	// windowCapCeiling is as long as a window is held open, in seconds: a week,
	// after which the next deploy has waited longer than any release should.
	windowCapCeiling = 7 * 24 * 60 * 60
	// kCeiling is as many windows as one service holds open, whatever the
	// evidence. Every increment is one more release a rollback undoes.
	kCeiling = 5
	// windowsPerK is how many windows closing without harm it takes to raise K by
	// one.
	windowsPerK = 3
)

// Learn is the table the score supplies, computed from every outcome in the
// store: the starting value of each parameter, and a row for each subject an
// outcome has moved it for. It reads records and writes none — what writes is
// [Writer.Ensure], which appends the version this table is a field of.
func Learn(ctx context.Context, pool *pgxpool.Pool) (SuppliedValues, error) {
	e, err := ReadEvidence(ctx, pool)
	if err != nil {
		return nil, err
	}
	return LearnFrom(e)
}

// LearnFrom is [Learn] over evidence already read. It is separate so that the
// rules are testable against a graph without reading one twice, and so that a
// caller printing what moved and a caller appending a version read the store once
// between them.
func LearnFrom(e *Evidence) (SuppliedValues, error) {
	values := StartingValues()

	bounds := attemptBounds(e)
	values = append(values, bounds...)
	values = append(values, itemSizeTargets(e, boundOf(bounds))...)
	values = append(values, thresholds(e)...)
	values = append(values, ks(e)...)

	sizes, err := windowParameters(e)
	if err != nil {
		return nil, err
	}
	return append(values, sizes...), nil
}

// thresholds is the risk threshold per gate row. Both halves of the rule are read
// off the same closed decisions: what the score auto-passed on the number, and
// what it auto-passed because its own sample selected the item.
func thresholds(e *Evidence) []Supplied {
	start, _ := Starting(gatepolicy.RiskThreshold)
	var moved []Supplied
	for _, row := range e.GateRows() {
		lowestBad := math.NaN()
		good, bad := 0, 0
		for _, f := range e.firings {
			if f.Opening.Gate != row || f.HumanClosed {
				continue
			}
			outcome := e.Outcome(f.Opening.ItemID)
			switch {
			case f.Closing.AutoPassedBy == AutoPassedByThreshold && outcome == OutcomeBadly:
				bad++
				if math.IsNaN(lowestBad) || f.Opening.Number < lowestBad {
					lowestBad = f.Opening.Number
				}
			case f.Closing.AutoPassedBy == AutoPassedBySample && outcome == OutcomeWell:
				good++
			}
		}

		value, why := start.Value, ""
		switch {
		case !math.IsNaN(lowestBad):
			value = math.Max(thresholdFloor, lowestBad-thresholdBand)
			why = fmt.Sprintf("%d change(s) auto-passed on the number at this row turned out badly, the lowest of them scoring %.2f, so the threshold is one band below it",
				bad, lowestBad)
		case good >= heldOutPerBand:
			bands := good / heldOutPerBand
			value = math.Min(thresholdCeiling, start.Value+float64(bands)*thresholdBand)
			why = fmt.Sprintf("%d held-out firing(s) at this row turned out well and none badly, which is %d band(s) of unbiased evidence that the gate was not needed",
				good, bands)
		}
		if why != "" && value != start.Value {
			moved = append(moved, Supplied{Parameter: gatepolicy.RiskThreshold, Subject: row, Value: value, Why: why})
		}
	}
	return moved
}

// attemptBounds is the attempt bound per stage: one above the highest attempt at
// which that stage ever produced work that got past it.
func attemptBounds(e *Evidence) []Supplied {
	start, _ := Starting(gatepolicy.AttemptBound)
	var moved []Supplied
	for _, stage := range e.Stages() {
		highest := e.succeededAt(stage)
		value := math.Min(attemptBoundCeiling, math.Max(start.Value, float64(highest+1)))
		if value == start.Value {
			continue
		}
		moved = append(moved, Supplied{
			Parameter: gatepolicy.AttemptBound, Subject: string(stage), Value: value,
			Why: fmt.Sprintf("this stage has produced work that got past it on attempt %d, so a retry after it is worth having", highest),
		})
	}
	return moved
}

// boundOf is the bound the item-size target's rule reads, which is the value this
// pass has just supplied for the stage or the starting value where it supplied
// none. The two rules run in one pass and in this order, so the target is halved
// against the bound the score supplies now and not against the one it supplied
// before the same pass moved it.
func boundOf(bounds []Supplied) func(item.Stage) float64 {
	start, _ := Starting(gatepolicy.AttemptBound)
	return func(stage item.Stage) float64 {
		for _, b := range bounds {
			if b.Subject == string(stage) {
				return b.Value
			}
		}
		return start.Value
	}
}

// itemSizeTargets is the item-size target per area, halved per stall.
//
// The value moves and nothing reads it: no cut sizes anything yet, so an area
// whose target has halved twice cuts exactly as it did before. That is worth
// supplying anyway — the movement is what a later cut reads, and an owner can see
// today that the score thinks this area's items are too large.
func itemSizeTargets(e *Evidence, bound func(item.Stage) float64) []Supplied {
	start, _ := Starting(gatepolicy.ItemSizeTarget)
	var moved []Supplied
	for _, area := range e.Areas() {
		stalls := e.stalls(area, bound)
		if len(stalls) == 0 {
			continue
		}
		value := math.Max(itemSizeFloor, start.Value/math.Pow(2, float64(len(stalls))))
		if value == start.Value {
			continue
		}
		moved = append(moved, Supplied{
			Parameter: gatepolicy.ItemSizeTarget, Subject: area, Value: value,
			Why: fmt.Sprintf("%d item(s) in this area reached the attempt bound at a stage and never shipped, which is what a cut too coarse spends and throws away", len(stalls)),
		})
	}
	return moved
}

// windowParameters is the watch window's three per service: the size and the
// confidence moved by what the comparison missed, and the cap moved by how long
// that service's own windows took to resolve.
func windowParameters(e *Evidence) ([]Supplied, error) {
	size, _ := Starting(gatepolicy.WindowSize)
	confidence, _ := Starting(gatepolicy.WindowConfidence)
	cap_, _ := Starting(gatepolicy.WindowCap)

	var moved []Supplied
	for _, service := range e.Services() {
		misses := e.Misses(service)
		falseCleans := 0
		for _, w := range misses {
			if w.Exit == window.ExitClean {
				falseCleans++
			}
		}

		if len(misses) > 0 {
			value := math.Max(windowSizeFloor, size.Value/math.Pow(2, float64(len(misses))))
			if value != size.Value {
				moved = append(moved, Supplied{
					Parameter: gatepolicy.WindowSize, Subject: service, Value: value,
					Why: fmt.Sprintf("%d window(s) on this service closed without harm over a release an incident was raised against, so the size they watched at was too coarse — and catching something this much smaller takes %.0f times the traffic",
						len(misses), math.Pow(4, float64(len(misses)))),
				})
			}
		}
		if falseCleans > 0 {
			value := math.Min(windowConfidenceCeiling, 1-(1-confidence.Value)/math.Pow(2, float64(falseCleans)))
			if value != confidence.Value {
				moved = append(moved, Supplied{
					Parameter: gatepolicy.WindowConfidence, Subject: service, Value: value,
					Why: fmt.Sprintf("%d window(s) on this service closed clean over a release an incident was raised against, so the boundary said it had ruled out what it had not", falseCleans),
				})
			}
		}

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
			value := math.Min(windowCapCeiling, math.Max(cap_.Value, 2*longest.Seconds()))
			if value != cap_.Value {
				moved = append(moved, Supplied{
					Parameter: gatepolicy.WindowCap, Subject: service, Value: value,
					Why: fmt.Sprintf("the longest window of this service to close on evidence took %s, and a cap under what a window actually needed closes unresolved one that would have resolved", longest),
				})
			}
		}
	}
	return moved, nil
}

// ks is K per service, folded over that service's own history in order. The fold
// and not a count, because the two kinds of evidence are not commutative: three
// windows closing without harm after a rollback are a service earning its
// throughput back, and the same three before it are a service that had it and
// lost it.
func ks(e *Evidence) []Supplied {
	start, _ := Starting(gatepolicy.K)
	var moved []Supplied
	for _, service := range e.Services() {
		k := start.Value
		noHarm, rollbacks := 0, 0
		since := 0
		for _, event := range e.serviceHistory(service) {
			switch {
			case event.noHarm:
				noHarm++
				since++
				if since >= windowsPerK {
					since = 0
					k = math.Min(kCeiling, k+1)
				}
			case event.sweeping:
				rollbacks++
				since = 0
				k = math.Max(start.Value, k-1)
			}
		}
		if k == start.Value {
			continue
		}
		moved = append(moved, Supplied{
			Parameter: gatepolicy.K, Subject: service, Value: k,
			Why: fmt.Sprintf("%d window(s) of this service closed without harm and %d rollback(s) swept a release, folded in order", noHarm, rollbacks),
		})
	}
	return moved
}
