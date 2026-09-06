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

// TestTheAuthoredDefaultIsTheValueInForceAndTheNumberSuppliesNoneOverIt: the
// default is a field of production's environment record, and what an owner
// authored is the value in force — the score supplies a strategy where they
// authored none and never over one. A safeguard and the held-out sample still
// add a control over an authored default, both adding rather than replacing.
func TestTheAuthoredDefaultIsTheValueInForceAndTheNumberSuppliesNoneOverIt(t *testing.T) {
	below := Assessment{ControlBound: ShippedControlBound, DiscountedImpact: 0}
	over := Assessment{ControlBound: ShippedControlBound, DiscountedImpact: ShippedControlBound}
	authored := Rollout{
		ReplacesReleaseID:       "rel_000000000000000000000000000000a",
		EveryTargetServesAShare: true,
		Default:                 StrategyWithControl,
	}

	pick := PickStrategy(below, authored)
	if pick.Strategy != StrategyWithControl || pick.Schedule != ScheduleWidened {
		t.Errorf("the pick under an authored default is %+v, want the row with a control", pick)
	}
	if pick.Why != WhyAuthored {
		t.Errorf("the pick reads %q beside the strategy, want the authored default", pick.Why)
	}

	// The default authored the other way is the value in force too: a number at
	// or above the bound supplies nothing over it, which is what an unauthored
	// default and an authored one being indistinguishable costs.
	without := authored
	without.Default = StrategyWithoutControl
	for _, a := range []Assessment{below, over} {
		pick := PickStrategy(a, without)
		if pick.Strategy != StrategyWithoutControl || pick.Schedule != "" {
			t.Errorf("the pick at %.2f under a default without a control is %+v, want the row without one",
				a.DiscountedImpact, pick)
		}
		if pick.Why != WhyAuthored {
			t.Errorf("the pick reads %q beside the strategy, want the authored default", pick.Why)
		}
	}

	// An irreversible area bounds the schedule and never the row, so it does not
	// reach a default authored without a control.
	irreversible := without
	irreversible.Irreversible = true
	if pick := PickStrategy(over, irreversible); pick.Strategy != StrategyWithoutControl {
		t.Errorf("the pick in an irreversible area under that default is %+v, want the authored row", pick)
	}

	// Both things that add a control add it over the authored default, each
	// naming what added it.
	safeguarded := without
	safeguarded.KeepsAControl = true
	if pick := PickStrategy(below, safeguarded); pick.Strategy != StrategyWithControl || pick.Why != WhySafeguarded {
		t.Errorf("the safeguarded pick over that default is %+v, want a control the safeguard added", pick)
	}
	heldOut := without
	heldOut.HeldOut = true
	if pick := PickStrategy(below, heldOut); pick.Strategy != StrategyWithControl || pick.Why != WhyHeldOut {
		t.Errorf("the held-out pick over that default is %+v, want a control the sample added", pick)
	}

	// Where nobody authored one the score supplies the strategy, and a pick
	// nothing bounded names no bound.
	unauthored := authored
	unauthored.Default = ""
	if pick := PickStrategy(below, unauthored); pick.Strategy != StrategyWithoutControl || pick.Why != "" {
		t.Errorf("the pick with nothing authored is %+v, want the row without a control and no bound named", pick)
	}
	if pick := PickStrategy(over, unauthored); pick.Strategy != StrategyWithControl || pick.Why != "" {
		t.Errorf("the pick at the bound with nothing authored is %+v, want the row with a control", pick)
	}
}
