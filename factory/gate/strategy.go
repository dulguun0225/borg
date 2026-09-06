package gate

import (
	"errors"
	"fmt"
	"slices"

	"github.com/dulguun0225/borg/factory/score"
)

// The rollout strategy, picked at the production deploy row and at no other. It
// attaches to a production deploy alone: what a strategy decides is whether a
// control runs, and a control is a comparison against organic traffic, which
// only production has.

// Strategy is how a release takes live traffic from the build it replaces. Two
// rows and not four, because they differ on one axis and everything downstream
// reads that axis alone: whether the build being replaced is still serving.
type Strategy string

const (
	// StrategyWithControl keeps the build being replaced serving the rest of
	// the traffic throughout, on the schedule beside it.
	StrategyWithControl Strategy = "with a control"
	// StrategyWithoutControl takes all of the traffic, in place, with none of
	// the build it replaces left running.
	StrategyWithoutControl Strategy = "without a control"
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
	ScheduleWidened Schedule = "a share widened as the comparison stays clear"
	// ScheduleFixed is a share kept fixed while the two are compared.
	ScheduleFixed Schedule = "a share kept fixed while the two are compared"
	// ScheduleAllAtOnce is all of the traffic at once, switched to a second
	// complete copy running beside the one it replaces.
	ScheduleAllAtOnce Schedule = "all of it at once, to a second complete copy running beside the one it replaces"
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

// The reasons a pick was bounded by something other than the score, in the words
// the open event stores.
const (
	// WhyFirstRelease is a service's first release, which has no control
	// whatever the score prefers: there is no build being replaced, so nothing
	// can keep serving beside it.
	WhyFirstRelease = "the service's first release has no build to keep serving beside it"
	// WhyPlatformServesNoShare is a target whose platform moves instances
	// rather than traffic. Serving a share means deciding what fraction of
	// arriving traffic reaches each of two builds, and a platform that cannot do
	// it offers no comparison to make.
	WhyPlatformServesNoShare = "a target of this environment is behind a platform that serves no share"
	// WhyIrreversible is an area whose hazard severity is irreversible, at which
	// only the widening schedule is available.
	WhyIrreversible = "the item's area is irreversible, so only the widening schedule is available"
	// WhyHeldOut is the score's own sample, which takes a strategy that keeps a
	// control wherever there is a build to start one from, whatever the vector's
	// impact half would have picked: the sample exists to produce outcome
	// evidence, and a deploy without a control leaves only the weak fallback.
	WhyHeldOut = "the score held this item out, and a held-out release keeps a control wherever one can run"
)

var (
	// ErrStrategyUnknown is returned for a strategy outside [Strategies].
	ErrStrategyUnknown = errors.New("gate: that is not a rollout strategy")
	// ErrScheduleUnknown is returned for a schedule outside [Schedules], and
	// for a schedule named on the row without a control, which has none.
	ErrScheduleUnknown = errors.New("gate: that is not a schedule of the row with a control")
)

// ControlBound is the impact discounted by reversibility at or above which the
// row with a control is picked. The strategy reads mostly impact against
// reversibility, which is the one number the score already reduces those two to,
// and the bound is published here because the number a human argues with has to
// be readable beside the pick.
//
// What it costs: the design states which half of the vector the strategy reads
// and not where the line falls, so this value is the code's and not the
// document's. A service that wants a control on every release gets one from a
// safeguard on the strategy rather than from this number.
const ControlBound = 0.5

// pickStrategy is what the row writes onto the open event: the row with a
// control where a control can run and the discounted impact is at or above
// [ControlBound], and the row without one otherwise.
//
// Order: a service with no build being replaced has no control to run, and a
// platform that serves no share cannot run one either, so neither reaches the
// number at all. A held-out release keeps a control wherever one can run,
// whatever the number preferred. An irreversible area bounds the schedule and
// never the row.
//
// The schedule is always the widening one. The other two are in the vocabulary
// because the design names three and the deployer performs whichever is on the
// open event, and what would choose between them — how fast a problem would
// appear on this service — is a reading the design names and nothing computes
// yet. Until something does, every controlled rollout widens as the comparison
// stays clear, which is the schedule an irreversible area is already held to.
func pickStrategy(a score.Assessment, replaces string, everyTargetServesAShare, heldOut, irreversible bool) (Pick, error) {
	if replaces == "" {
		return Pick{Strategy: StrategyWithoutControl, Why: WhyFirstRelease}, nil
	}
	if !everyTargetServesAShare {
		return Pick{Strategy: StrategyWithoutControl, Why: WhyPlatformServesNoShare}, nil
	}
	pick := Pick{Strategy: StrategyWithoutControl}
	switch {
	case heldOut:
		pick = Pick{Strategy: StrategyWithControl, Schedule: ScheduleWidened, Why: WhyHeldOut}
	case a.DiscountedImpact >= ControlBound:
		pick = Pick{Strategy: StrategyWithControl, Schedule: ScheduleWidened}
	}
	// Why names the one bound that applied, so a bound already named is not
	// overwritten: a held-out release in an irreversible area was bounded by the
	// sample, and the schedule an irreversible area holds it to is the one every
	// controlled rollout already takes.
	if irreversible && pick.Strategy == StrategyWithControl && pick.Why == "" {
		pick.Why = WhyIrreversible
	}
	return pick, nil
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
