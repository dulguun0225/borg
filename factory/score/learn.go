package score

import (
	"context"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/boundary"
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
exit and the read it closed on, a rollback and the releases it swept, an incident against a release,
and the attempts a stage took.

Every value moves by harm on one side and by its own stated cost on the other. Gate policy's own table
says what goes wrong at each end of each of them, and both ends are the evidence: one end is something
going wrong, the other is the parameter costing more than it returns. Where a value moves one way only,
the reason is that its loose end is not observable here, and that reason is stated with it.

The risk threshold is the one value whose loose end is observable and is deliberately not read. How
often a row puts a human at it is a count, and it is what that row's loose end costs — but it is not
evidence about safety. A window too fine to resolve rules nothing out, so loosening it gives up
nothing; a human at a gate reads the change and stops some of them, so loosening the threshold on its
cost alone would be the score deciding that human review is not worth its price. That is an owner's
judgment, made by authoring the value. What raises a threshold here is the held-out sample and nothing
else, and a factory whose sample never selects has a threshold that can fall and cannot rise.

  risk threshold      per gate row. It falls to one band (0.05) below the lowest number the score
                      auto-passed on the number at that row and whose item turned out badly, floored
                      at 0.05 — so the next change scoring what that one scored is decided by a human.
                      It rises one band per 3 held-out firings at that row whose items turned out well,
                      ceilinged at 0.90, and nothing else raises it: a gated change a human approved is
                      not evidence the gate was unnecessary, because the human's own scrutiny is part
                      of why it turned out well. A gated change that turned out well is consistent with
                      three things at once — that it was never risky, that the human caught what would
                      have made it risky, and that its author was more careful for knowing a human
                      would read it — and no record tells those apart. The sample is the only thing
                      that produces one that does, which is why it is the only evidence for a rise.
                      A fall outranks a rise: it names a number the score is known to have got wrong.

  attempt limit       per stage, both ways, once 3 items have reported at that stage. One above the
                      highest attempt at which the stage produced work that got past it, floored at 2
                      and ceilinged at 6 — so a stage that has needed a third attempt gets a fourth,
                      and a stage nothing has ever needed more than one attempt at keeps one retry and
                      not two. The floor is the design's own reasoning about a reply the protocol
                      refused, which one retry is what covers.

  item-size target    per area. Halved per stall in that area — an item whose attempts at a stage
                      reached the limit above and which has no release, which is work spent and thrown
                      away — floored at 50. One way only: the other end of a bad size is cost per
                      feature and rework rate, and cost per feature needs features counted, which
                      nothing here does. Nothing reads the value either, until a decomposition sizes something.

  watch window size   per service, and the coarser of two numbers. What harm asks for is the starting
                      size halved per miss — a window closed cleared or timed out over a release an
                      incident was later raised against, which is the crossing the health monitor could
                      have seen and did not — floored at 0.002. What the traffic reaches is the finest
                      size on that same lattice whose units to clean this service's newest closed
                      window's read supplies inside the cap in force, and never coarser than the
                      starting size. The size in force is the coarser of the two, because a size finer
                      than the traffic can resolve rules nothing out: the window ends at the cap every
                      time, protects nothing, and holds the next deploy for the whole cap. The two are
                      different questions — what is worth catching, and whether anything can be caught
                      — and only the first is about harm.

  window confidence   per service. Each false clearing — a miss whose window closed cleared rather
                      than timing out — halves the distance from the confidence in force to
                      one, ceilinged at 0.999. One way only, and the reason is arithmetic: the units a
                      window needs grow as the log of one over one-minus-confidence, where they grow as
                      the inverse square of the size, so tightening this costs about a doubling where
                      two halvings of the size cost sixteen times. It does not compound.

  watch window cap    per service, both ways. Twice the longest a window of that service took to close
                      on evidence — cleared or condemned, the two exits that read the quantity rather than the
                      clock — floored at 60 and ceilinged at a week. A cap under what a window actually
                      needed closes unresolved one that would have resolved; a cap far above it holds
                      the next deploy for nothing.

  window limit        per service. Folded over that service's own history in order: from 1, every 3
                      windows closing without condemning a release raise it by one, ceilinged at 5, and a rollback that
                      swept a release lowers it by one and resets the count, floored at 1. Windows
                      closing without condemning a release are one-sided evidence — a service that has never rolled
                      back has seen the throughput a higher limit gives and none of the rollback size
                      it causes — which is why the rise is slow and the fall is immediate.

  allowed predicate   nothing. No outcome teaches which kinds of assertion a consumer contract may draw
  kinds               from, so the score supplies none and the value in force is the kinds this
                      factory can decide, what an owner authored, and what a safeguard adds.

The held-out sample is how the threshold gets evidence its own decisions did not select. One firing in
ten that the score would have gated is held out instead: the item auto-passes every gate the score
would have gated from that firing onward, its closing row says the sample and not the threshold passed
it, and its release is watched to the cap rather than stopping where the boundary would allow. It
reaches no row a safeguard reached, because a human a safeguard added at a gate is a human an owner
added and no mechanism in the design removes one. What the sample cannot have on this substrate is the strategy that
keeps a control: every deploy here moves a process rather than traffic, so a held-out release is
watched by the same confounded comparison as every other one and the longest watch available is all
the sample gets.

Two of the seven move one way, and each says above why its loose end is not observable here. What that
costs is that those two ratchet: an install tightens them and only an owner authoring over the value
loosens anything. Neither compounds — the confidence's cost grows as a log, and nothing reads the
item-size target yet — which is what makes a ratchet on those two tolerable where a ratchet on the
window's size was not.
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
	// attemptLimitCeiling is as high as the limit goes, whatever the evidence: a
	// stage retried more than this has stopped being a stage that retries.
	attemptLimitCeiling = 6
	// attemptLimitFloor is as low as it goes. One retry is the one the design's own
	// reasoning is about — a stage that failed once has usually had a reply the
	// protocol refused — so a limit of two survives whatever the evidence says.
	attemptLimitFloor = 2
	// attemptLimitEvidence is how many items must have reported at a stage before
	// the limit moves off its starting value at all. One item that got past a stage
	// first time is not grounds for supplying a limit the whole factory reads.
	attemptLimitEvidence = 3
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
	// windowCapFloor is as short as it goes. A cap under a minute would end a
	// window before traffic had arrived to read, which is not a cap but a refusal
	// to watch.
	windowCapFloor = 60
	// windowLimitCeiling is as many windows as one service holds open, whatever the
	// evidence. Every increment is one more release a rollback undoes.
	windowLimitCeiling = 5
	// windowsPerRaise is how many windows closing without condemning a release it takes to raise the
	// window limit by one.
	windowsPerRaise = 3
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

	limits := attemptLimits(e)
	values = append(values, limits...)
	values = append(values, itemSizeTargets(e, limitOf(limits))...)
	values = append(values, thresholds(e)...)
	values = append(values, windowLimits(e)...)

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
			case f.Closing.WhyItAutoPassed == AutoPassThreshold && outcome == OutcomeBadly:
				bad++
				if math.IsNaN(lowestBad) || f.Opening.Number < lowestBad {
					lowestBad = f.Opening.Number
				}
			case f.Closing.WhyItAutoPassed == AutoPassSample && outcome == OutcomeWell:
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

// attemptLimits is the attempt limit per stage, and it moves both ways: up to one
// above the highest attempt at which that stage ever produced work that got past
// it, and down where every item that got past it did so on fewer attempts than the
// limit allows. The loose end is the one gate policy's own table states — a limit
// higher than the evidence spends agent time before anyone sees the item — and it
// is observable in the per-stage rows, so there is no reason for this one to be
// one-way.
//
// Nothing moves until [attemptLimitEvidence] items have reported at the stage, and
// the floor is [attemptLimitFloor]: one retry is what the design's reasoning about
// a refused reply is for, and no amount of evidence takes that away.
func attemptLimits(e *Evidence) []Supplied {
	start, _ := Starting(gatepolicy.AttemptLimit)
	var moved []Supplied
	for _, stage := range e.Stages() {
		reached := e.reachedStage(stage)
		if reached < attemptLimitEvidence {
			continue
		}
		highest := e.succeededAt(stage)
		if highest == 0 {
			// Nothing has got past this stage at all, so there is no attempt that
			// worked and nothing to read either way.
			continue
		}
		value := math.Min(attemptLimitCeiling, math.Max(attemptLimitFloor, float64(highest+1)))
		if value == start.Value {
			continue
		}
		how := "so a retry after it is worth having"
		if value < start.Value {
			how = "and nothing has ever needed more, so the attempts above that are spent before anybody sees the item"
		}
		moved = append(moved, Supplied{
			Parameter: gatepolicy.AttemptLimit, Subject: string(stage), Value: value,
			Why: fmt.Sprintf("over %d item(s) at this stage the highest attempt that produced work getting past it is %d, %s",
				reached, highest, how),
		})
	}
	return moved
}

// limitOf is the limit the item-size target's rule reads, which is the value this
// pass has just supplied for the stage or the starting value where it supplied
// none. The two rules run in one pass and in this order, so the target is halved
// against the limit the score supplies now and not against the one it supplied
// before the same pass moved it.
func limitOf(limits []Supplied) func(item.Stage) float64 {
	start, _ := Starting(gatepolicy.AttemptLimit)
	return func(stage item.Stage) float64 {
		for _, b := range limits {
			if b.Subject == string(stage) {
				return b.Value
			}
		}
		return start.Value
	}
}

// itemSizeTargets is the item-size target per area, halved per stall.
//
// The value moves and nothing reads it: no decomposition sizes anything yet, so an area
// whose target has halved twice decomposes exactly as it did before. That is worth
// supplying anyway — the movement is what a later decomposition reads, and an owner can see
// today that the score thinks this area's items are too large.
func itemSizeTargets(e *Evidence, limit func(item.Stage) float64) []Supplied {
	start, _ := Starting(gatepolicy.ItemSizeTarget)
	var moved []Supplied
	for _, area := range e.Areas() {
		stalls := e.stalls(area, limit)
		if len(stalls) == 0 {
			continue
		}
		value := math.Max(itemSizeFloor, start.Value/math.Pow(2, float64(len(stalls))))
		if value == start.Value {
			continue
		}
		moved = append(moved, Supplied{
			Parameter: gatepolicy.ItemSizeTarget, Subject: area, Value: value,
			Why: fmt.Sprintf("%d item(s) in this area reached the attempt limit at a stage and never shipped, which is what a decomposition too coarse spends and throws away", len(stalls)),
		})
	}
	return moved
}

// windowParameters is the watch window's three per service. Two of them move both
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
// is why [_The watch window_]'s restriction to evidence traceable to the health
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
		falseClearings := 0
		for _, w := range misses {
			if w.Exit == window.ExitCleared {
				falseClearings++
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

		if confidenceValue := 1 - (1-confidence.Value)/math.Pow(2, float64(falseClearings)); falseClearings > 0 {
			value := math.Min(windowConfidenceCeiling, confidenceValue)
			if value != confidence.Value {
				moved = append(moved, Supplied{
					Parameter: gatepolicy.WindowConfidence, Subject: service, Value: value,
					Why: fmt.Sprintf("%d window(s) on this service closed cleared over a release an incident was raised against, so the boundary said it had ruled out what it had not", falseClearings),
				})
			}
		}

		// The size: the coarser of what harm asks for and what the traffic reaches.
		wanted := math.Max(windowSizeFloor, size.Value/math.Pow(2, float64(len(misses))))
		why := ""
		if len(misses) > 0 {
			why = fmt.Sprintf("%d window(s) on this service closed without condemning a release over a release an incident was raised against, so the size they watched at was too coarse",
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
// windows closing without condemning a release after a rollback are a service earning its
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
			Why: fmt.Sprintf("%d window(s) of this service closed without condemning a release and %d rollback(s) swept a release, folded in order", noHarm, rollbacks),
		})
	}
	return moved
}
