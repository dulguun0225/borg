package deploy_test

import (
	"context"
	"testing"

	"github.com/dulguun0225/borg/factory/deploy"
)

type queueReadings struct {
	held         map[string]deploy.HoldKind
	awaited      map[string]bool
	dependencies map[string][]deploy.DependencyReading
}

func (r queueReadings) ReadWindow(_ context.Context, _ string) (deploy.WindowReading, error) {
	return deploy.WindowReading{Limit: 1}, nil
}

func (r queueReadings) ReadBudget(_ context.Context, _ string) (bool, error) { return false, nil }

func (r queueReadings) ReadRollback(_ context.Context, c deploy.QueueCandidate) (deploy.RollbackReading, error) {
	for itemID := range r.awaited {
		if itemID == c.ItemID {
			return deploy.RollbackReading{Holding: true, RevertIntentID: "revert-intent"}, nil
		}
	}
	return deploy.RollbackReading{}, nil
}

func (r queueReadings) ReadDependencies(_ context.Context, c deploy.QueueCandidate) ([]deploy.DependencyReading, error) {
	return r.dependencies[c.ItemID], nil
}

func (r queueReadings) ReadDrift(_ context.Context, _ string) (bool, error) { return false, nil }

func (r queueReadings) ReadHuman(_ context.Context, c deploy.QueueCandidate) (bool, error) {
	return r.held[c.ItemID] != deploy.HoldNone, nil
}

func TestQueueOrderUsesReleaseNumberAndTheAwaitedRevertException(t *testing.T) {
	ordered, err := deploy.QueueOrder(context.Background(), "svc", []deploy.QueueCandidate{
		{ItemID: "held", ServiceID: "svc", ReleaseID: "rel-1", ReleaseNumber: 1},
		{ItemID: "release", ServiceID: "svc", ReleaseID: "rel-2", ReleaseNumber: 2},
		{ItemID: "revert", IntentID: "revert-intent", ServiceID: "svc", ReleaseID: "rel-3", ReleaseNumber: 3},
		{ItemID: "release", ServiceID: "svc", ReleaseID: "rel-2", ReleaseNumber: 2},
		{ItemID: "deployed", ServiceID: "svc", ReleaseID: "rel-4", ReleaseNumber: 4, DeployID: "dep-4"},
		{ItemID: "other", ServiceID: "other", ReleaseID: "rel-0", ReleaseNumber: 0},
	}, deploy.QueueReadings{
		Window: queueReadings{},
		Budget: queueReadings{}, Rollback: queueReadings{awaited: map[string]bool{"revert": true}}, Dependency: queueReadings{},
		Drift: queueReadings{}, Human: queueReadings{held: map[string]deploy.HoldKind{"held": deploy.HoldHuman}},
	})
	if err != nil {
		t.Fatalf("QueueOrder: %v", err)
	}
	if len(ordered) != 3 || ordered[0].ItemID != "revert" || ordered[1].ItemID != "held" || ordered[2].ItemID != "release" {
		t.Fatalf("QueueOrder = %#v, want revert then held and release in release-number order", ordered)
	}
	if ordered[0].HoldReason != deploy.HoldNone || !ordered[0].AwaitedRevert {
		t.Fatalf("awaited revert = %#v, want no hold and awaited revert", ordered[0])
	}
	if ordered[1].HoldReason != deploy.HoldHuman {
		t.Fatalf("held release reason = %q, want %q", ordered[1].HoldReason, deploy.HoldHuman)
	}
}

type queueConditions struct {
	window       deploy.WindowReading
	budget       bool
	rollback     deploy.RollbackReading
	dependencies []deploy.DependencyReading
	drift        bool
	human        bool
}

func (r queueConditions) ReadWindow(context.Context, string) (deploy.WindowReading, error) {
	return r.window, nil
}
func (r queueConditions) ReadBudget(context.Context, string) (bool, error) { return r.budget, nil }
func (r queueConditions) ReadRollback(context.Context, deploy.QueueCandidate) (deploy.RollbackReading, error) {
	return r.rollback, nil
}
func (r queueConditions) ReadDependencies(context.Context, deploy.QueueCandidate) ([]deploy.DependencyReading, error) {
	return r.dependencies, nil
}
func (r queueConditions) ReadDrift(context.Context, string) (bool, error) { return r.drift, nil }
func (r queueConditions) ReadHuman(context.Context, deploy.QueueCandidate) (bool, error) {
	return r.human, nil
}

func TestQueueHoldDecidesEachCondition(t *testing.T) {
	tests := []struct {
		name string
		set  func(*queueConditions)
		want deploy.HoldKind
	}{
		{"window", func(r *queueConditions) { r.window = deploy.WindowReading{Open: 2, Limit: 2} }, deploy.HoldWindow},
		{"budget", func(r *queueConditions) { r.budget = true }, deploy.HoldBudget},
		{"rollback", func(r *queueConditions) { r.rollback.Holding = true }, deploy.HoldRollback},
		{"dependency", func(r *queueConditions) {
			r.dependencies = []deploy.DependencyReading{{RequiredItemID: "item-1", CurrentItemID: "item-2"}}
		}, deploy.HoldDependency},
		{"drift", func(r *queueConditions) { r.drift = true }, deploy.HoldDrift},
		{"human", func(r *queueConditions) { r.human = true }, deploy.HoldHuman},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			readings := queueConditions{window: deploy.WindowReading{Open: 0, Limit: 1}}
			test.set(&readings)
			decision, err := (deploy.QueueReadings{
				Window: readings, Budget: readings, Rollback: readings,
				Dependency: readings, Drift: readings, Human: readings,
			}).Held(context.Background(), deploy.QueueCandidate{ServiceID: "svc", ItemID: "item"})
			if err != nil {
				t.Fatalf("Held: %v", err)
			}
			if !decision.Held || decision.Why != test.want {
				t.Fatalf("Held = %+v, want %s", decision, test.want)
			}
		})
	}
}

func TestAwaitedRevertIsNeverHeldAndGoesFirst(t *testing.T) {
	readings := queueConditions{
		window:       deploy.WindowReading{Open: 2, Limit: 2},
		budget:       true,
		rollback:     deploy.RollbackReading{Holding: true, RevertIntentID: "revert-intent"},
		dependencies: []deploy.DependencyReading{{RequiredItemID: "item-1", CurrentItemID: "item-2"}},
		drift:        true,
		human:        false,
	}
	composed := deploy.QueueReadings{
		Window: readings, Budget: readings, Rollback: readings,
		Dependency: readings, Drift: readings, Human: readings,
	}
	revert := deploy.QueueCandidate{
		ItemID: "revert", IntentID: "revert-intent", ServiceID: "svc",
		ReleaseID: "rel-revert", ReleaseNumber: 2,
	}
	decision, err := composed.Held(context.Background(), revert)
	if err != nil {
		t.Fatalf("Held: %v", err)
	}
	if decision.Held || decision.Why != deploy.HoldNone || !decision.AwaitedRevert {
		t.Fatalf("Held = %+v, want an unheld awaited revert", decision)
	}
	ordered, err := deploy.QueueOrder(context.Background(), "svc", []deploy.QueueCandidate{
		revert, {ItemID: "release", ServiceID: "svc", ReleaseID: "rel-release", ReleaseNumber: 1},
	}, composed)
	if err != nil {
		t.Fatalf("QueueOrder: %v", err)
	}
	if len(ordered) == 0 || ordered[0].ItemID != revert.ItemID {
		t.Fatalf("QueueOrder = %#v, want the awaited revert first", ordered)
	}
	if ordered[0].HoldReason != deploy.HoldNone || !ordered[0].AwaitedRevert {
		t.Fatalf("awaited revert in queue = %#v, want no hold", ordered[0])
	}
}

func TestAwaitedRevertKeepsAPendingHumanDecision(t *testing.T) {
	readings := queueConditions{
		rollback: deploy.RollbackReading{Holding: true, RevertIntentID: "revert-intent"},
		human:    true,
	}
	decision, err := (deploy.QueueReadings{
		Window: readings, Budget: readings, Rollback: readings,
		Dependency: readings, Drift: readings, Human: readings,
	}).Held(context.Background(), deploy.QueueCandidate{
		ItemID: "revert", IntentID: "revert-intent", ServiceID: "svc",
	})
	if err != nil {
		t.Fatalf("Held: %v", err)
	}
	if !decision.Held || decision.Why != deploy.HoldHuman || !decision.AwaitedRevert {
		t.Fatalf("Held = %+v, want the pending human decision", decision)
	}
}
