package healthmonitor

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/window"
)

// crossedElsewhere is the reading a brownout's window takes beside the
// producer's own numbers, and nil for every other window. A brownout's effect
// lands wherever the hidden read is: a disabled operation still called errs in
// the producer's own error rate, which the window already reads, but a field a
// consumer parses and now fails on errs in that consumer's numbers alone, and
// the producer's own never show it.
//
// So the reading is one already taken — every service read against its own
// recent history — and any service crossing it while the brownout's window is
// open fails that window. Nothing is attributed across services and no call is
// recorded: the crossing is evidence that something the brownout could have
// broken did break, and the window fails in the safe direction, an element
// restored that nobody read and a removal a human must re-raise.
//
// What it costs is a false failure whose rate rises with the install's size — a
// service breaking for its own reasons fails a brownout that touched nothing of
// its — and that rate is a number rather than an unknown: every service's
// traffic over its own run length in force for as long as the window is open.
func (h *HealthMonitor) crossedElsewhere(ctx context.Context, w Watching, win window.Window) (*Crossing, error) {
	if h.brownouts == nil || win.ReleaseID == "" {
		return nil, nil
	}
	isBrownout, err := h.brownouts.IsBrownout(ctx, win.ReleaseID)
	if err != nil {
		return nil, fmt.Errorf("healthmonitor: reading whether release %s is a brownout: %w", win.ReleaseID, err)
	}
	if !isBrownout {
		return nil, nil
	}

	all, err := service.All(ctx, h.pool)
	if err != nil {
		return nil, err
	}
	for _, svc := range all {
		if svc.Retired() {
			continue
		}
		crossing, err := h.ownHistoryOf(ctx, w, win, svc)
		if err != nil {
			return nil, err
		}
		if crossing != nil {
			return crossing, nil
		}
	}
	return nil, nil
}

// ownHistoryOf is one service read against its own recent history, of the
// release running in production there. It is the reading the control check
// already takes, run over another service: the sizes and the run length are the
// ones in force today rather than the brownout window's own, because what is
// being read is that service's behaviour and not this window's release.
//
// A service running nothing, or one whose deploy has not completed, is read as
// nothing crossing: there are no instances to have a history against.
func (h *HealthMonitor) ownHistoryOf(ctx context.Context, w Watching, win window.Window,
	svc service.Service) (*Crossing, error) {
	targets := targetsOrDefault(svc.Targets, w)
	current, running, err := deploy.Current(ctx, h.pool, svc.ID, w.EnvironmentID, targets)
	if err != nil || !running || current.ReleaseID == "" {
		return nil, err
	}
	rel, err := release.Get(ctx, h.pool, current.ReleaseID)
	if err != nil {
		return nil, err
	}
	elsewhere := Watching{ID: svc.ID, Name: svc.Name, EnvironmentID: w.EnvironmentID}
	return h.ownHistory(ctx, elsewhere, win, Arm{BuildID: rel.BuildID, DeployID: current.ID},
		h.readings.OwnHistorySize, h.readings.OwnHistoryRunLength, targets, KindOwnHistory)
}
