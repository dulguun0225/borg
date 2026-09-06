package gate

import (
	"context"
	"errors"
	"fmt"

	"github.com/dulguun0225/borg/factory/record"
)

// Safeguard the strategy, which is the production deploy row's fourth action and
// the one that is not a verdict: a human at the row places the safeguard that
// keeps a control, and the deploy this row decides then runs with the build it
// replaces still serving. It is one of the three safeguards that add rather than
// clamp, so nothing here bounds a number.

// StrategySafeguard is what the composition supplies for that action: the write
// that places the safeguard on this service. It is an interface because a
// safeguard is package policy's write at Factory and this package writes no
// record of its own — the same arrangement [Holds] and [DriftDetector] have.
type StrategySafeguard interface {
	// KeepAControl places the safeguard that keeps a control on one service, as
	// the human who asked for it at the row.
	KeepAControl(ctx context.Context, actor record.Actor, serviceID string) error
	// KeepsAControl is whether such a safeguard stands on the service, read at
	// every production deploy firing the way every other check a gate makes is
	// read at the moment of firing. It is on this interface and not a second one
	// because the record is one record: what places it and what reads it are the
	// same seam.
	KeepsAControl(ctx context.Context, serviceID string) (bool, error)
}

var (
	// ErrStrategyNotPickedHere is returned by [Gate.SafeguardTheStrategy] at a
	// row that picks no rollout strategy. A strategy attaches to a production
	// deploy and to no other, so there is nothing to safeguard anywhere else.
	ErrStrategyNotPickedHere = errors.New("gate: this row picks no rollout strategy, so there is none to safeguard")
	// ErrPlatformServesNoShare is the one refusal the design allows this action:
	// a platform that moves instances rather than traffic cannot run a control,
	// so the safeguard would bind a strategy that cannot be performed.
	ErrPlatformServesNoShare = errors.New("gate: this platform serves no share, so there is no control for a safeguard to keep")
	// ErrStrategySafeguardNotComposed is returned where a gate composed with no
	// writer for this safeguard is asked for the action. It is a departure from
	// the design's one refusal, and gate's doc.go says so.
	ErrStrategySafeguardNotComposed = errors.New("gate: this gate has no writer for the safeguard that keeps a control")
)

// SafeguardTheStrategy is the action itself: it places the safeguard that keeps
// a control on the firing's service and returns the strategy the deploy runs
// under with that safeguard in force, which is the row with a control on the
// widening schedule — the schedule every controlled rollout takes.
//
// It is refused at a row that picks no strategy and on a platform that serves no
// share, and on nothing else: the design gives this action one bound and it is
// the platform's. A service's first release is not a refusal — the safeguard is
// placed and binds from the second — but its pick stands as it was, there being
// no build being replaced for a control to be.
func (g *Gate) SafeguardTheStrategy(ctx context.Context, opened Opened, actor record.Actor) (Pick, error) {
	if opened.Gate.Kind != KindDeployToProduction {
		return Pick{}, fmt.Errorf("%w: %s", ErrStrategyNotPickedHere, opened.Gate)
	}
	if opened.Strategy.Why == WhyPlatformServesNoShare {
		return Pick{}, fmt.Errorf("%w: %s", ErrPlatformServesNoShare, opened.Subject.EnvironmentID)
	}
	if g.strategySafeguard == nil {
		return Pick{}, fmt.Errorf("%w: %s", ErrStrategySafeguardNotComposed, opened.Subject.ServiceID)
	}
	if err := g.strategySafeguard.KeepAControl(ctx, actor, opened.Subject.ServiceID); err != nil {
		return Pick{}, fmt.Errorf("gate: placing the safeguard that keeps a control on %s: %w",
			opened.Subject.ServiceID, err)
	}
	if opened.Strategy.Why == WhyFirstRelease {
		return opened.Strategy, nil
	}
	return Pick{Strategy: StrategyWithControl, Schedule: ScheduleWidened, Why: WhySafeguarded}, nil
}
