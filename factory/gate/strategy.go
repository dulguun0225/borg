package gate

import (
	"errors"
	"fmt"
	"slices"

	"github.com/dulguun0225/borg/factory/score"
)

// The rollout strategy as the open event stores it. The score picks it — the
// same number that decides the gate decides the strategy, and the bound it is
// picked against is a value the score version names — and this is the shape the
// pick is written in and read back from, which the deployer reads off the event.

// Strategy is how a release takes live traffic from the build it replaces. Two
// rows and not four, because they differ on one axis and everything downstream
// reads that axis alone: whether the build being replaced is still serving.
type Strategy string

const (
	// StrategyWithControl keeps the build being replaced serving the rest of
	// the traffic throughout, on the schedule beside it.
	StrategyWithControl = Strategy(score.StrategyWithControl)
	// StrategyWithoutControl takes all of the traffic, in place, with none of
	// the build it replaces left running.
	StrategyWithoutControl = Strategy(score.StrategyWithoutControl)
)

// Strategies is both rows.
var Strategies = []Strategy{StrategyWithControl, StrategyWithoutControl}

// Schedule is an attribute of the row with a control and not a strategy of its
// own: canary, A/B and blue-green are that row at three schedules. They differ
// in how much traffic the release is exposed to and not in what is provisioned.
type Schedule string

const (
	// ScheduleWidened is a share widened as the comparison stays clear. It is
	// the only schedule available in an area whose hazard severity is
	// irreversible, the other two moving the rest of the traffic in one step
	// rather than as a reading clears it.
	ScheduleWidened = Schedule(score.ScheduleWidened)
	// ScheduleFixed is a share kept fixed while the two are compared.
	ScheduleFixed = Schedule(score.ScheduleFixed)
	// ScheduleAllAtOnce is all of the traffic at once, switched to a second
	// complete copy running beside the one it replaces.
	ScheduleAllAtOnce = Schedule(score.ScheduleAllAtOnce)
)

// Schedules is every schedule the row with a control may take.
var Schedules = []Schedule{ScheduleWidened, ScheduleFixed, ScheduleAllAtOnce}

// Pick is the strategy this row picked, and the schedule where it picked the row
// with a control. It goes on the open event for the deployer to read, and the
// production deploy record names the strategy the deployer performed beside it.
type Pick struct {
	Strategy Strategy
	// Schedule is empty on the row without a control, which has none.
	Schedule Schedule
	// Why is what bounded the pick where something did, in words a human reads
	// beside the strategy: no build to keep serving, a platform that serves no
	// share, an irreversible area, or the held-out sample.
	Why string
}

// The reasons a pick was bounded by something other than the number, named from
// the score's own so that the words a human reads beside the strategy are the
// words the component that picked it wrote.
const (
	// WhyFirstRelease is a service's first release, which has no control
	// whatever the score prefers.
	WhyFirstRelease = score.WhyFirstRelease
	// WhyPlatformServesNoShare is a target whose platform moves instances
	// rather than traffic.
	WhyPlatformServesNoShare = score.WhyPlatformServesNoShare
	// WhyIrreversible is an area whose hazard severity is irreversible, at which
	// only the widening schedule is available.
	WhyIrreversible = score.WhyIrreversible
	// WhyHeldOut is the score's own sample, which takes a strategy that keeps a
	// control wherever there is a build to start one from.
	WhyHeldOut = score.WhyHeldOut
)

var (
	// ErrStrategyUnknown is returned for a strategy outside [Strategies].
	ErrStrategyUnknown = errors.New("gate: that is not a rollout strategy")
	// ErrScheduleUnknown is returned for a schedule outside [Schedules], and
	// for a schedule named on the row without a control, which has none.
	ErrScheduleUnknown = errors.New("gate: that is not a schedule of the row with a control")
)

// pickedBy is the score's pick in the words this package stores it in. The score
// decides — the strategy reads mostly impact against reversibility, which is the
// one number the score already reduces those two to, against the bound the score
// version names — and this is where the answer is copied onto the open event.
func pickedBy(a score.Assessment, r score.Rollout) Pick {
	picked := score.PickStrategy(a, r)
	return Pick{
		Strategy: Strategy(picked.Strategy),
		Schedule: Schedule(picked.Schedule),
		Why:      picked.Why,
	}
}

// Validate refuses a pick naming a strategy or a schedule this package does not
// own, and one naming a schedule on the row without a control, which has none.
// It is what a reader of an open event — the deployer above all — checks a
// stored pick with, so a value some other writer put there is refused rather
// than performed.
func (p Pick) Validate() error {
	if p.Strategy == "" && p.Schedule == "" {
		return nil
	}
	if !slices.Contains(Strategies, p.Strategy) {
		return fmt.Errorf("%w: %q", ErrStrategyUnknown, p.Strategy)
	}
	if p.Strategy == StrategyWithControl && !slices.Contains(Schedules, p.Schedule) {
		return fmt.Errorf("%w: %q", ErrScheduleUnknown, p.Schedule)
	}
	if p.Strategy == StrategyWithoutControl && p.Schedule != "" {
		return fmt.Errorf("%w: the row without a control named %q", ErrScheduleUnknown, p.Schedule)
	}
	return nil
}
