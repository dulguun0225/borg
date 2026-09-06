package healthmonitor

import (
	"context"
	"fmt"
	"math"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/window"
)

// readBeside is the two readings that run beside the comparison and can fail the
// same release: the reading against the service's own recent history, and an
// explicit threshold where the service has an objective to read one against.
//
// It also reads the control the same way, and that reading takes passed away
// rather than failing anything: a comparison rules a change out relative to the
// control, so a control that is itself failing lets a bad release pass. Failed
// stays available — a release worse than a failing control still improves
// production when rolled back.
func (h *HealthMonitor) readBeside(ctx context.Context, w Watching, svc service.Service,
	win window.Window, into *Watched) error {
	crossing, err := h.ownHistory(ctx, w, win, Arm{BuildID: win.BuildID, DeployID: win.DeployID},
		win.OwnHistorySize, win.OwnHistoryRunLength, win.Targets, KindOwnHistory)
	if err != nil {
		return err
	}
	if crossing != nil {
		into.Evaluated.Crossed = crossing
		return nil
	}

	if into.HasBaseline {
		control := h.baselineArm(ctx, win, into.Baseline)
		into.ControlCrossing, err = h.ownHistory(ctx, w, win, control,
			win.OwnHistorySize, win.OwnHistoryRunLength, win.Targets, KindOwnHistory)
		if err != nil {
			return err
		}
	}

	crossing, err = h.threshold(ctx, w, svc, win)
	if err != nil {
		return err
	}
	if crossing != nil {
		into.Evaluated.Crossed = crossing
	}
	return nil
}

// ownHistory reads one arm against what that service was doing before the deploy
// that placed it, rather than against instances running beside it. It has no
// second arm to move with it, which is what catches a change to shared state
// that moves both arms of the comparison together.
//
// It never closes, so it has no last look to spend a rate against and states an
// average run length instead. The size is one value per quantity, as the
// window's is: the smallest change in that quantity this reading has to detect.
func (h *HealthMonitor) ownHistory(ctx context.Context, w Watching, win window.Window, of Arm,
	sizes map[gatepolicy.Quantity]float64, runLength float64, targets []string,
	kind CrossingKind) (*Crossing, error) {
	if len(sizes) == 0 || runLength <= 1 || !of.Named() {
		return nil, nil
	}
	comparisons := max(len(targets), 1) * len(sizes)
	boundaryFor := func(q gatepolicy.Quantity) (boundary.Boundary, bool) {
		size, carried := sizes[q]
		if !carried {
			return boundary.Boundary{}, false
		}
		b, err := boundary.AtRunLength(size, runLength, comparisons, window.Worse(q))
		return b, err == nil
	}

	var read Evaluated
	for _, target := range targets {
		series, err := h.emission.History(ctx, History{ServiceName: w.Name, Target: target, Of: of})
		if err != nil {
			return nil, fmt.Errorf("healthmonitor: reading %s against its own recent history on %s: %w", w.Name, target, err)
		}
		if err := evaluate(boundaryFor, win.Power, target, series, kind, &read); err != nil {
			return nil, err
		}
	}
	return read.Crossed, nil
}

// threshold is the explicit threshold: an absolute number the service's error
// rate is read against, where the comparison is relative. It applies in addition
// to the comparison rather than instead of it, so it can only add a check.
//
// Absolute is what the number is and not what the reading is: whether a rate
// exceeds a fixed number is decided from a finite count of requests, so a
// service whose true value sits near that number crosses it eventually however
// good the service is. So it is read against a boundary valid at every point,
// with an average run length in force, and not as a count compared against a
// number — which is why the other arm here is the number itself, held over the
// same units the release served.
//
// The number is what a safeguard set on the service record, per quantity. Where
// no safeguard set one there is no threshold, and a service's first release is
// then unmeasured and nothing about it is discovered by watching — there being
// no build in production to compare it against either.
func (h *HealthMonitor) threshold(ctx context.Context, w Watching, svc service.Service,
	win window.Window) (*Crossing, error) {
	if len(win.ThresholdSize) == 0 || win.ThresholdRunLength <= 1 || len(svc.ExplicitThreshold) == 0 {
		return nil, nil
	}
	allowed := map[gatepolicy.Quantity]float64{}
	for quantity, threshold := range svc.ExplicitThreshold {
		if _, named := win.ThresholdSize[quantity]; named {
			allowed[quantity] = threshold.Number
		}
	}
	if len(allowed) == 0 {
		return nil, nil
	}
	comparisons := max(len(win.Targets), 1) * len(win.ThresholdSize)
	boundaryFor := func(q gatepolicy.Quantity) (boundary.Boundary, bool) {
		size, carried := win.ThresholdSize[q]
		if !carried {
			return boundary.Boundary{}, false
		}
		b, err := boundary.AtRunLength(size, win.ThresholdRunLength, comparisons, window.Worse(q))
		return b, err == nil
	}

	var read Evaluated
	for _, target := range win.Targets {
		series, err := h.emission.History(ctx, History{
			ServiceName: w.Name, Target: target,
			Of: Arm{BuildID: win.BuildID, DeployID: win.DeployID},
		})
		if err != nil {
			return nil, fmt.Errorf("healthmonitor: reading %s against its threshold on %s: %w", w.Name, target, err)
		}
		if err := evaluate(boundaryFor, win.Power, target, against(series, allowed),
			KindExplicitThreshold, &read); err != nil {
			return nil, err
		}
	}
	return read.Crossed, nil
}

// against replaces the other arm of every interval with the threshold itself:
// the same units the release served, at exactly the number the safeguard stated
// for that quantity. That is what makes an absolute number readable by a
// boundary built for two arms — the number is an arm that behaves exactly as
// authored, so the statistic is the release's exceedance of it and nothing else.
//
// Only the quantities a threshold was set on are kept, since a threshold set on
// one quantity says nothing about another.
func against(series Series, allowed map[gatepolicy.Quantity]float64) Series {
	stated := series
	stated.Operations = nil
	for _, operation := range series.Operations {
		quantities := map[gatepolicy.Quantity]boundary.Observed{}
		for quantity, observed := range operation.Quantities {
			number, named := allowed[quantity]
			if !named {
				continue
			}
			var replaced boundary.Observed
			for _, counts := range observed.Intervals {
				counts.BaselineUnits = counts.Units
				counts.BaselineCount = int64(math.Round(number * float64(counts.Units)))
				replaced.Intervals = append(replaced.Intervals, counts)
			}
			quantities[quantity] = replaced
		}
		stated.Operations = append(stated.Operations, OperationSeries{
			Operation: operation.Operation, Quantities: quantities,
		})
	}
	return stated
}
