package score

import (
	"context"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lease"
)

// Learned is everything one pass over the outcomes computes: the table of values
// the score supplies, the bands of the number, the drift readings, and the false
// alarms published per human. All four are fields of the version the pass
// appends, so a decision naming a version can be read against what the score
// then knew.
type Learned struct {
	Supplied    SuppliedValues
	Bands       []Band
	Drift       []Drift
	FalseAlarms []FalseAlarm
}

// Learn is what the score supplies and what it published beside it, computed
// from every outcome in the store. It reads records and writes none — what
// writes is [Writer.Ensure], which appends the version this is a field of.
func Learn(ctx context.Context, pool *pgxpool.Pool, token lease.Token, marks Marks) (Learned, error) {
	e, err := ReadEvidence(ctx, pool, token, marks)
	if err != nil {
		return Learned{}, err
	}
	return LearnFrom(e)
}

// LearnFrom is [Learn] over evidence already read. It is separate so that the
// rules are testable against a graph without reading one twice, and so that a
// caller printing what moved and a caller appending a version read the store once
// between them.
func LearnFrom(e *Evidence) (Learned, error) {
	values := StartingValues()

	limits := attemptLimits(e)
	values = append(values, limits...)
	values = append(values, itemSizeTargets(e, limitOf(limits))...)
	values = append(values, thresholds(e)...)
	values = append(values, windowLimits(e)...)

	windows, err := windowParameters(e)
	if err != nil {
		return Learned{}, err
	}
	values = append(values, windows...)

	return Learned{
		Supplied:    values,
		Bands:       e.bands(),
		Drift:       e.drift(),
		FalseAlarms: e.falseAlarms(),
	}, nil
}

// thresholds is the risk threshold per gate row. Both halves of the rule are read
// off the same closed decisions: what the score auto-passed on the number, and
// what it auto-passed because its own sample selected the item.
//
// Only a window that closed passed makes an item well and only one that failed
// makes it badly, so a held-out release whose window timed out raises nothing:
// the sample exists to produce an outcome, and the exit that reports none reports
// none here either.
func thresholds(e *Evidence) []Supplied {
	start, _ := Starting(gatepolicy.RiskThreshold)
	var moved []Supplied
	for _, row := range e.GateRows() {
		lowestBad := math.NaN()
		good, bad := 0, 0
		for _, f := range e.firings {
			if f.OpenEvent.Gate != row || f.HumanClosed {
				continue
			}
			outcome := e.Outcome(f.OpenEvent.ItemID)
			switch {
			case f.CloseEvent.WhyItAutoPassed == AutoPassThreshold && outcome == OutcomeBadly:
				bad++
				if math.IsNaN(lowestBad) || f.OpenEvent.Number < lowestBad {
					lowestBad = f.OpenEvent.Number
				}
			case f.CloseEvent.WhyItAutoPassed == AutoPassSample && outcome == OutcomeWell:
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
			why = fmt.Sprintf("%d held-out firing(s) at this row reached a window that closed passed and none that failed, which is %d band(s) of unbiased evidence that the gate was not needed",
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

// itemSizeTargets is the item-size target per area, halved per stall, in the
// count of the intent's requirements an item answers — the unit decomposition
// sets and the unit an owner authors in.
func itemSizeTargets(e *Evidence, limit func(item.Stage) float64) []Supplied {
	start, _ := Starting(gatepolicy.ItemSizeTarget)
	var moved []Supplied
	for _, area := range e.Areas() {
		stalls := e.stalls(area, limit)
		if len(stalls) == 0 {
			continue
		}
		value := math.Max(itemSizeFloor, math.Round(start.Value/math.Pow(2, float64(len(stalls)))))
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
