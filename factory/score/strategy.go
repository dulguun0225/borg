package score

// The rollout strategy, which is the second of the three things one number
// decides: the gate reads mostly likelihood against impact, the strategy reads
// mostly impact against reversibility, and the boundary of the analysis window
// reads mostly impact. It attaches to a production deploy and to no other,
// because what a strategy decides is whether a control runs and a control is a
// comparison against organic traffic, which only production has.

// Strategy is how a release takes live traffic from the build it replaces. Two
// rows and not four, because they differ on one axis and everything downstream
// reads that axis alone: whether the build being replaced is still serving.
type Strategy string

const (
	// StrategyWithControl keeps the build being replaced serving the rest of the
	// traffic throughout, on the schedule beside it.
	StrategyWithControl Strategy = "with a control"
	// StrategyWithoutControl takes all of the traffic, in place, with none of the
	// build it replaces left running.
	StrategyWithoutControl Strategy = "without a control"
)

// Schedule is an attribute of the row with a control and not a strategy of its
// own: canary, A/B and blue-green are that row at three schedules. They differ
// in how much traffic the release is exposed to and not in what is provisioned.
type Schedule string

const (
	// ScheduleWidened is a share widened as the comparison stays clear. It is the
	// only schedule available in an area whose hazard severity is irreversible,
	// the other two moving the rest of the traffic in one step rather than as a
	// reading clears it.
	ScheduleWidened Schedule = "a share widened as the comparison stays clear"
	// ScheduleFixed is a share kept fixed while the two are compared.
	ScheduleFixed Schedule = "a share kept fixed while the two are compared"
	// ScheduleAllAtOnce is all of the traffic at once, switched to a second
	// complete copy running beside the one it replaces.
	ScheduleAllAtOnce Schedule = "all of it at once, to a second complete copy running beside the one it replaces"
)

// The reasons a pick was decided by something other than the number, in the
// words the open event stores.
const (
	// WhyFirstRelease is a service's first release, which has no control whatever
	// the score prefers: there is no build being replaced, so nothing can keep
	// serving beside it.
	WhyFirstRelease = "the service's first release has no build to keep serving beside it"
	// WhyPlatformServesNoShare is a target whose platform moves instances rather
	// than traffic. Serving a share means deciding what fraction of arriving
	// traffic reaches each of two builds, and a platform that cannot do it offers
	// no comparison to make.
	WhyPlatformServesNoShare = "a target of this environment is behind a platform that serves no share"
	// WhyIrreversible is an area whose hazard severity is irreversible, at which
	// only the widening schedule is available.
	WhyIrreversible = "the item's area is irreversible, so only the widening schedule is available"
	// WhyHeldOut is the score's own sample, which takes a strategy that keeps a
	// control wherever there is a build to start one from, whatever the vector's
	// impact half would have picked: the sample exists to produce outcome
	// evidence, and a deploy without a control leaves only the weak fallback.
	WhyHeldOut = "the score held this item out, and a held-out release keeps a control wherever one can run"
	// WhySafeguarded is a safeguard on the strategy default keeping a control on
	// this service. It adds the control rather than clamping a number, so it
	// bounds the pick wherever a control can run at all and whatever the number
	// preferred.
	WhySafeguarded = "a safeguard on this service keeps a control"
	// WhyAuthored is a strategy default an owner authored on production's
	// environment record. The score supplies a strategy where an owner authored
	// none and never over one, so an authored default is the value in force and
	// the number decided nothing.
	WhyAuthored = "an owner authored this environment's strategy default"
)

// Whys is every reason a pick was decided by something other than the number,
// in the order the constants above declare them. It exists so that package
// gate, which mirrors these constants onto the open event, can be checked
// against this list rather than against a reader's memory.
var Whys = []string{
	WhyFirstRelease, WhyPlatformServesNoShare, WhyIrreversible,
	WhyHeldOut, WhySafeguarded, WhyAuthored,
}

// ShippedControlBound is the impact discounted by reversibility at or above
// which the product ships picking the row with a control. What is in force is
// the bound the score version names, which is this one until something ships
// another.
//
// The design states which half of the vector the strategy reads and not where
// the line falls, so this value is the code's and not the document's. A service
// that wants a control on every release gets one from a safeguard on the
// strategy rather than from this number.
const ShippedControlBound = 0.5

// Pick is the strategy one production deploy takes, and the schedule where the
// row with a control was picked. The gate writes it onto the open event and the
// production deploy record names the strategy the deployer performed beside it.
type Pick struct {
	Strategy Strategy
	// Schedule is empty on the row without a control, which has none.
	Schedule Schedule
	// Why is what decided the pick where something other than the number did, in
	// words a human reads beside the strategy: no build to keep serving, a
	// platform that serves no share, an irreversible area, the held-out sample,
	// a safeguard, or the default an owner authored.
	Why string
}

// Rollout is what the pick is made over besides the vector: the release being
// replaced, whether every target of the environment serves a share, whether the
// score's own sample selected the item, and whether the item's area is graded
// irreversible. All four are the caller's reads, being facts of records the
// score does not weigh a factor from.
type Rollout struct {
	ReplacesReleaseID       string
	EveryTargetServesAShare bool
	HeldOut                 bool
	Irreversible            bool
	// Default is the rollout strategy an owner authored on production's
	// environment record, and is empty where they authored none. Where it names
	// a row that row is the value in force and the number supplies nothing over
	// it; where it is empty the score supplies the strategy.
	Default Strategy
	// KeepsAControl is whether a safeguard on the strategy default stands on
	// this service. A safeguard there adds a control rather than clamping a
	// number, so it picks the row with a control wherever one can run.
	KeepsAControl bool
}

// PickStrategy is the score picking the rollout strategy: the row with a control
// where a control can run and the discounted impact is at or above the bound the
// assessment's own version names, and the row without one otherwise.
//
// Order: a service with no build being replaced has no control to run, and a
// platform that serves no share cannot run one either, so neither reaches the
// number at all. A safeguard keeping a control on the service is next: it adds
// rather than clamps, so it picks the row with a control wherever one can run
// and whatever the number preferred. A held-out release does the same, for the
// sample's own reason. The strategy default an owner authored on production's
// environment record comes next and the number last: the value in force is what
// an owner authored where they authored one and what the score supplies
// otherwise, so an authored row is taken whichever way it points and the number
// reaches only a service whose default is empty. An irreversible area bounds the
// schedule and never the row.
//
// The schedule is always the widening one. The other two are in the vocabulary
// because the design names three and the deployer performs whichever is on the
// open event, and what would choose between them — how fast a problem would
// appear on this service — is a reading the design names and nothing computes
// yet. Until something does, every controlled rollout widens as the comparison
// stays clear, which is the schedule an irreversible area is already held to.
func PickStrategy(a Assessment, r Rollout) Pick {
	if r.ReplacesReleaseID == "" {
		return Pick{Strategy: StrategyWithoutControl, Why: WhyFirstRelease}
	}
	if !r.EveryTargetServesAShare {
		return Pick{Strategy: StrategyWithoutControl, Why: WhyPlatformServesNoShare}
	}
	pick := Pick{Strategy: StrategyWithoutControl}
	switch {
	case r.KeepsAControl:
		pick = Pick{Strategy: StrategyWithControl, Schedule: ScheduleWidened, Why: WhySafeguarded}
	case r.HeldOut:
		pick = Pick{Strategy: StrategyWithControl, Schedule: ScheduleWidened, Why: WhyHeldOut}
	case r.Default == StrategyWithControl:
		pick = Pick{Strategy: StrategyWithControl, Schedule: ScheduleWidened, Why: WhyAuthored}
	case r.Default == StrategyWithoutControl:
		pick = Pick{Strategy: StrategyWithoutControl, Why: WhyAuthored}
	case a.DiscountedImpact >= boundOf(a):
		pick = Pick{Strategy: StrategyWithControl, Schedule: ScheduleWidened}
	}
	// Why names the one thing that decided the row, so a reason already named is
	// not overwritten: a held-out release in an irreversible area was decided by
	// the sample, and the schedule an irreversible area holds it to is the one
	// every controlled rollout already takes.
	if r.Irreversible && pick.Strategy == StrategyWithControl && pick.Why == "" {
		pick.Why = WhyIrreversible
	}
	return pick
}

// boundOf is the bound the assessment was computed under, falling back to the
// shipped one for an assessment produced before the bound was a field of a
// version and for the four rows that read no factor set at all.
func boundOf(a Assessment) float64 {
	if a.ControlBound <= 0 {
		return ShippedControlBound
	}
	return a.ControlBound
}
