// The two readings of the pick that are not the number: the strategy default an
// owner authored on production's environment record, and a safeguard on that
// default keeping a control.
package score

import "testing"

// TestASafeguardKeepsAControlWhateverTheNumberPreferred: a safeguard on the
// strategy default adds a control rather than clamping a number, so it picks the
// row with a control wherever one can run and says so beside the strategy.
func TestASafeguardKeepsAControlWhateverTheNumberPreferred(t *testing.T) {
	below := Assessment{ControlBound: ShippedControlBound, DiscountedImpact: 0}
	replacing := Rollout{
		ReplacesReleaseID:       "rel_000000000000000000000000000000a",
		EveryTargetServesAShare: true,
	}

	if pick := PickStrategy(below, replacing); pick.Strategy != StrategyWithoutControl {
		t.Fatalf("with nothing authored and nothing safeguarded the pick is %+v, want the row without a control", pick)
	}

	safeguarded := replacing
	safeguarded.KeepsAControl = true
	pick := PickStrategy(below, safeguarded)
	if pick.Strategy != StrategyWithControl || pick.Schedule != ScheduleWidened {
		t.Errorf("the safeguarded pick is %+v, want the row with a control on the widening schedule", pick)
	}
	if pick.Why != WhySafeguarded {
		t.Errorf("the pick's bound reads %q, want the safeguard's", pick.Why)
	}

	// The two refusals the safeguard does not reach: a service's first release
	// has no build to keep serving beside it, and a platform that serves no
	// share offers no comparison to make.
	first := safeguarded
	first.ReplacesReleaseID = ""
	if pick := PickStrategy(below, first); pick.Why != WhyFirstRelease {
		t.Errorf("the first release of a safeguarded service picks %+v, want no control", pick)
	}
	noShare := safeguarded
	noShare.EveryTargetServesAShare = false
	if pick := PickStrategy(below, noShare); pick.Why != WhyPlatformServesNoShare {
		t.Errorf("a safeguarded service on a platform serving no share picks %+v, want no control", pick)
	}
}

// TestTheAuthoredDefaultIsWhatProductionTakesWhereNothingNarrowsThePick: the
// default is a field of production's environment record, and the pick starts
// from it. Nothing bounded such a pick, so it names no bound.
func TestTheAuthoredDefaultIsWhatProductionTakesWhereNothingNarrowsThePick(t *testing.T) {
	below := Assessment{ControlBound: ShippedControlBound, DiscountedImpact: 0}
	authored := Rollout{
		ReplacesReleaseID:       "rel_000000000000000000000000000000a",
		EveryTargetServesAShare: true,
		Default:                 StrategyWithControl,
	}

	pick := PickStrategy(below, authored)
	if pick.Strategy != StrategyWithControl || pick.Schedule != ScheduleWidened {
		t.Errorf("the pick under an authored default is %+v, want the row with a control", pick)
	}
	if pick.Why != "" {
		t.Errorf("the pick names the bound %q, and nothing bounded it", pick.Why)
	}

	// The default authored the other way is what the score already does where
	// nobody authored one: the number decides.
	without := authored
	without.Default = StrategyWithoutControl
	if pick := PickStrategy(below, without); pick.Strategy != StrategyWithoutControl {
		t.Errorf("the pick under a default without a control is %+v, want the row without one", pick)
	}
	over := Assessment{ControlBound: ShippedControlBound, DiscountedImpact: ShippedControlBound}
	if pick := PickStrategy(over, without); pick.Strategy != StrategyWithControl {
		t.Errorf("a number at the bound under a default without a control picks %+v, want the row with one", pick)
	}
}
