package healthmonitor

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/window"
)

// Open opens the analysis window over one production deploy, and returns false
// where no window opens. A window is one per production deploy of a release the
// service has not watched before, whichever attempt that is — so a rollback
// opens none, the release it returns to having been watched already, and neither
// does a redeploy of one already watched.
//
// Everything in force is read here and copied onto the record, which is what
// makes a reading at an exit interpretable: an owner re-authoring a size while
// the window is open would otherwise change what a window already closed is read
// to have meant. So is whether the passed exit is available, which is a fact of
// the open and never changes.
//
// heldOut is the caller's to hand over for the reason the score version is: what
// the score selected is read off the decisions on the item, which this package
// does not read. A held-out release runs to the cap rather than stopping where
// the boundary would allow.
//
// Where the strategy kept a control, the deployer is asked to start one per
// target the rollout is planned to reach, running the build of the release a
// rollback of this one would return to — never the ordinal predecessor, whose
// build contains the change just failed wherever something was.
func (h *HealthMonitor) Open(ctx context.Context, w Watching, deployID, releaseID, scoreVersion string, heldOut bool) (window.Window, bool, error) {
	if err := w.validate(); err != nil {
		return window.Window{}, false, err
	}
	if scoreVersion == "" {
		return window.Window{}, false, ErrScoreVersionMissing
	}

	watched, found, err := window.ForRelease(ctx, h.pool, releaseID)
	if err != nil {
		return window.Window{}, false, err
	}
	if found {
		return watched, false, nil
	}

	rel, err := release.Get(ctx, h.pool, releaseID)
	if err != nil {
		return window.Window{}, false, err
	}
	svc, err := service.Get(ctx, h.pool, w.ID)
	if err != nil {
		return window.Window{}, false, err
	}
	dep, err := deploy.Get(ctx, h.pool, deployID)
	if err != nil {
		return window.Window{}, false, err
	}
	version, err := h.policy.Newest(ctx, Actor)
	if err != nil {
		return window.Window{}, false, err
	}

	// A service missing one of the four fields the deployer populates opens a
	// window that records only that it measures nothing, since what the field
	// would have supplied is what a reading needs to exist at all.
	if missing := unmeasurable(svc); missing != "" {
		opened, err := h.windows.Open(ctx, Actor, window.OpenEvent{
			DeployID: deployID, ReleaseID: releaseID, BuildID: rel.BuildID, ServiceID: w.ID,
			MeasuresNothing: true, BoundaryVersion: boundary.Version,
			PolicyVersion: version.ID, ScoreVersion: scoreVersion,
		})
		return opened, err == nil, err
	}

	target, hasTarget, err := h.TargetBelow(ctx, w, rel.Number)
	if err != nil {
		return window.Window{}, false, err
	}
	parameters, err := h.policy.WindowParameters(ctx, w.ID)
	if err != nil {
		return window.Window{}, false, err
	}
	opening, err := h.opening(ctx, w, svc, rel, dep, parameters, target, hasTarget)
	if err != nil {
		return window.Window{}, false, err
	}
	opening.DeployID, opening.ScoreVersion, opening.PolicyVersion = deployID, scoreVersion, version.ID
	opening.HeldOut = heldOut
	opening.PassedAvailable = opening.PassedAvailable && !heldOut

	opened, err := h.windows.Open(ctx, Actor, opening)
	if err != nil {
		return window.Window{}, false, err
	}
	if err := h.startControls(ctx, w, opened, dep, target, hasTarget); err != nil {
		return opened, true, err
	}
	return opened, true, nil
}

// opening resolves everything the window names at the open: the size and the
// power per quantity with the floors under each, the confidence, the cap, the
// target set the boundary is allocated over, which operations are read alone,
// the emission version each arm's series are read at, and the parameters of the
// two readings beside the comparison.
func (h *HealthMonitor) opening(ctx context.Context, w Watching, svc service.Service,
	rel release.Release, dep deploy.Deploy, parameters policyWindow,
	target release.Release, hasTarget bool) (window.OpenEvent, error) {
	o := window.OpenEvent{
		ReleaseID:           rel.ID,
		BuildID:             rel.BuildID,
		ServiceID:           w.ID,
		Size:                map[gatepolicy.Quantity]float64{},
		Power:               map[gatepolicy.Quantity]float64{},
		Confidence:          parameters.Confidence.Number,
		CapSeconds:          parameters.CapSeconds.Number,
		BoundaryVersion:     boundary.Version,
		Targets:             svc.Targets,
		OwnHistorySize:      h.readings.OwnHistorySize,
		OwnHistoryRunLength: h.readings.OwnHistoryRunLength,
	}
	if len(o.Targets) == 0 {
		// A service that named none runs on every target of the environment, which
		// this package does not read; the environment's own record is the
		// composition's to hand over, so the deploy's target stands for the set
		// until it does. doc.go says which caller is not built.
		o.Targets = []string{dep.EnvironmentID}
	}
	for _, quantity := range gatepolicy.Quantities {
		size := parameters.Size[quantity].Number
		power := parameters.Power[quantity].Number
		if size <= 0 || power <= 0 || power >= 1 {
			continue
		}
		o.Size[quantity], o.Power[quantity] = size, power
	}

	previous, err := h.previousRead(ctx, w.ID)
	if err != nil {
		return window.OpenEvent{}, err
	}
	o.OperationsReadAlone = operationsReadAlone(previous, o)
	o.PassedAvailable = hasTarget && passedReachable(previous, o)

	// The explicit threshold is what a safeguard set on the service record: an
	// absolute number per quantity and the size the owner set beside it. The
	// window copies the size and the run length it is read at, as it copies the
	// comparison's; the number itself stays on the record the safeguard wrote,
	// which is where a reader argues with it.
	for quantity, threshold := range svc.ExplicitThreshold {
		if o.ThresholdSize == nil {
			o.ThresholdSize = map[gatepolicy.Quantity]float64{}
		}
		o.ThresholdSize[quantity] = threshold.Size
		o.ThresholdRunLength = h.readings.ThresholdRunLength
	}

	releaseArm := Arm{BuildID: rel.BuildID, DeployID: dep.ID}
	o.EmissionVersionRelease, err = h.emission.Shape(ctx, releaseArm)
	if err != nil {
		return window.OpenEvent{}, err
	}
	if o.EmissionVersionRelease == "" {
		o.EmissionVersionRelease = newestEmissionVersion()
	}
	if hasTarget {
		baseline := Arm{BuildID: target.BuildID, DeployID: dep.ID}
		if o.EmissionVersionControl, err = h.emission.Shape(ctx, baseline); err != nil {
			return window.OpenEvent{}, err
		}
	}
	if o.EmissionVersionControl != "" {
		_, outside, err := ReadableAcross(o.EmissionVersionRelease, o.EmissionVersionControl)
		if err != nil {
			return window.OpenEvent{}, err
		}
		o.QuantitiesOutside = outside
		for _, quantity := range outside {
			delete(o.Size, quantity)
			delete(o.Power, quantity)
		}
	}
	return o, nil
}

// startControls asks the deployer for one control per target the rollout is
// planned to reach, running the build of the rollback's target. There is one per
// target so that both arms of every comparison sit in the same failure domain: a
// target losing its network takes the release's instances and its control
// together.
//
// Nothing is started where the strategy kept no control — there the comparison
// falls back to the release below on the target — or where the factory is
// composed with no deployer.
func (h *HealthMonitor) startControls(ctx context.Context, w Watching, opened window.Window,
	dep deploy.Deploy, target release.Release, hasTarget bool) error {
	if h.deployer == nil || dep.StrategyPerformed != deploy.StrategyWithControl || !hasTarget {
		return nil
	}
	for _, t := range opened.Targets {
		control := Control{
			ServiceID: w.ID, ServiceName: w.Name, EnvironmentID: w.EnvironmentID,
			DeployID: opened.DeployID, Target: t, BuildID: target.BuildID,
		}
		if err := h.deployer.StartControl(ctx, control); err != nil {
			return fmt.Errorf("healthmonitor: starting the control for %s on %s: %w", w.Name, t, err)
		}
	}
	return nil
}

// unmeasurable is which of the four fields the deployer populates the service is
// missing, and empty where it has all four.
func unmeasurable(svc service.Service) string {
	if !svc.Reachability.Written() {
		return "nothing has adopted this service, so none of the four fields is written"
	}
	for _, field := range []struct {
		what    string
		present bool
	}{
		{"a target it reaches", svc.Reachability.TargetReached},
		{"replaceable instances", svc.Reachability.InstancesReplaceable},
		{"a rollback path", svc.Reachability.RollbackPathPresent},
		{"a readable emission", svc.Reachability.EmissionReadable},
	} {
		if !field.present {
			return field.what
		}
	}
	return ""
}

// newestEmissionVersion is what a build the factory builds today emits, which is
// what a window names for an arm the store holds no record for yet.
func newestEmissionVersion() string { return EmissionShapes[len(EmissionShapes)-1].Version }

// Room is whether the service may open another window, how many it holds open,
// and what the window limit is. An open window blocks nothing until as many as
// the limit allows are open, and then the next production deploy holds — a wait
// on the factory, which does not page, so it shows only to a reader who asks.
//
// It is here rather than in whatever computes the hold, because the limit in
// force and the count of open windows are the two halves of one reading and a
// caller holding only one of them would report a hold it cannot explain.
func (h *HealthMonitor) Room(ctx context.Context, serviceID string) (bool, int, int, error) {
	if serviceID == "" {
		return false, 0, 0, fmt.Errorf("%w: the window limit is per service, and none is named", ErrWatchingIncomplete)
	}
	open, err := window.CountOpen(ctx, h.pool, serviceID)
	if err != nil {
		return false, 0, 0, err
	}
	parameters, err := h.policy.WindowParameters(ctx, serviceID)
	if err != nil {
		return false, 0, 0, err
	}
	limit := int(parameters.WindowLimit.Number)
	return open < limit, open, limit, nil
}
