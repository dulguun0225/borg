// Safeguard the strategy, the production deploy row's fourth action.
package gate_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/record"
)

// keepsAControl records the one call the action makes on the composition.
type keepsAControl struct {
	forService string
	by         string
	err        error
}

func (k *keepsAControl) KeepAControl(_ context.Context, actor record.Actor, serviceID string) error {
	k.forService, k.by = serviceID, actor.Key
	return k.err
}

// TestSafeguardTheStrategyPlacesTheSafeguardAndKeepsAControl: a human at the
// production deploy row may put a control on a rollout the score would have run
// without one, which is one of the three safeguards that add rather than clamp.
// The deploy then runs on the row with a control, on the widening schedule.
func TestSafeguardTheStrategyPlacesTheSafeguardAndKeepsAControl(t *testing.T) {
	placed := &keepsAControl{}
	s, p := &fakeScore{assessment: assessed(0.2)}, &fakePolicy{applied: applied(0.9)}
	ctx, pool, token, g := newGateWith(t, s, p, func(c *gate.Composition) {
		c.StrategySafeguard = placed
	})

	f := deployFiring(t, ctx, pool, token)
	// A release replacing another is what makes a control possible at all: the
	// build being replaced is what keeps serving beside it.
	f.ReplacesReleaseID = "rel_000000000000000000000000000000b"
	opened, err := g.Fire(ctx, f)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if opened.Strategy.Strategy != gate.StrategyWithoutControl {
		t.Fatalf("the score picked %+v, and this test needs the row without a control to add one to", opened.Strategy)
	}

	pick, err := g.SafeguardTheStrategy(ctx, opened, owner)
	if err != nil {
		t.Fatalf("SafeguardTheStrategy: %v", err)
	}
	if placed.forService != f.ServiceID || placed.by != owner.Key {
		t.Errorf("the safeguard was placed on %q by %q, want this service and the human at the row",
			placed.forService, placed.by)
	}
	if pick.Strategy != gate.StrategyWithControl || pick.Schedule != gate.ScheduleWidened {
		t.Errorf("the deploy runs under %+v, want the row with a control on the widening schedule", pick)
	}
	if pick.Why != gate.WhySafeguarded {
		t.Errorf("the pick says %q, want the reason a human put the control there", pick.Why)
	}
}

// TestSafeguardTheStrategyIsRefusedWhereThereIsNoControlToKeep: the design gives
// this action one bound and it is the platform's — a platform that moves
// instances rather than traffic cannot run a control at all. The other two
// refusals are a row that picks no strategy and a factory composed with no
// writer for the safeguard.
func TestSafeguardTheStrategyIsRefusedWhereThereIsNoControlToKeep(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.2)}, &fakePolicy{applied: applied(0.9)}
	ctx, _, _, g := newGateWith(t, s, p, func(c *gate.Composition) {
		c.StrategySafeguard = &keepsAControl{}
	})

	// A row that picks no strategy: a strategy attaches to a production deploy and
	// to no other.
	merged, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if _, err := g.SafeguardTheStrategy(ctx, merged, owner); !errors.Is(err, gate.ErrStrategyNotPickedHere) {
		t.Errorf("the action at %s = %v, want ErrStrategyNotPickedHere", merged.Gate, err)
	}

	// A platform that serves no share, which the pick already says.
	onNoShare := gate.Opened{
		Gate:     gate.DeployToProduction,
		Subject:  gate.Subjects{Row: gate.DeployToProduction, ServiceID: "svc_a", EnvironmentID: "env_a"},
		Strategy: gate.Pick{Strategy: gate.StrategyWithoutControl, Why: gate.WhyPlatformServesNoShare},
	}
	if _, err := g.SafeguardTheStrategy(ctx, onNoShare, owner); !errors.Is(err, gate.ErrPlatformServesNoShare) {
		t.Errorf("the action on a platform that serves no share = %v, want ErrPlatformServesNoShare", err)
	}

	// A factory composed with no writer for the safeguard refuses it, which is a
	// second refusal beside the platform's and is what gate's doc.go states.
	uncomposed := gate.New(gate.Composition{Pool: nil, Score: s, Policy: p})
	replacing := gate.Opened{
		Gate:     gate.DeployToProduction,
		Subject:  gate.Subjects{Row: gate.DeployToProduction, ServiceID: "svc_a"},
		Strategy: gate.Pick{Strategy: gate.StrategyWithoutControl},
	}
	if _, err := uncomposed.SafeguardTheStrategy(ctx, replacing, owner); !errors.Is(err,
		gate.ErrStrategySafeguardNotComposed) {
		t.Errorf("the action with no writer composed = %v, want ErrStrategySafeguardNotComposed", err)
	}
}
