// TakeOver, the Work screen's one call on [item.Dispatch.ClearEscalation]: a
// human taking an escalated item over is refused a stage later than the one it
// escalated from, the same one-way rule a reject or a rework request answers
// to everywhere else in the pipeline.
package main

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/screens"
	"github.com/dulguun0225/borg/factory/service"
)

// TestWorkTakeOverRefusesAnEscalationBypass escalates an item at Tasks and
// shows the take-over cannot return it past Tasks — to Implementation, a stage
// the escalation never authored the way to — while a stage at or above Tasks
// is still reachable, which is what a human backstopping the stage the factory
// gave up on, or something earlier it depends on, needs.
func TestWorkTakeOverRefusesAnEscalationBypass(t *testing.T) {
	ctx, d, out := newPath(t, approvals)
	s := newScreens(t, ctx, d, out)
	svc, found, err := service.ByName(ctx, d.pool, theService)
	if err != nil || !found {
		t.Fatalf("ByName(%s) = found %v, %v", theService, found, err)
	}
	in, err := s.p.intake.TakeIn(ctx, s.p.human, intent.Arrival{
		Source: intent.SourceOwner, Statement: theService + ": " + theStatement, ProjectID: s.p.projectID,
	})
	if err != nil {
		t.Fatalf("taking the intent in: %v", err)
	}
	it, err := item.NewDecomposition(d.pool, d.token).Create(ctx, decompositionActor,
		item.New{IntentID: in.ID, ServiceID: svc.ID, Branch: "item/bypass", RequirementsAnswered: oneRequirement},
		"", "", nil)
	if err != nil {
		t.Fatalf("decomposing the item: %v", err)
	}
	for _, stage := range []item.Stage{item.StageImplementationPlan, item.StageTasks} {
		if _, err := s.p.items.Advance(ctx, dispatch.Actor, it.ID, stage); err != nil {
			t.Fatalf("Advance to %s: %v", stage, err)
		}
	}
	if _, err := s.p.items.Escalate(ctx, dispatch.Actor, it.ID); err != nil {
		t.Fatalf("Escalate: %v", err)
	}

	who := principal.OfHuman(s.p.human.Key, record.BasisClaimed)

	// Implementation is one stage past Tasks, the stage the item escalated
	// from: taking it over there would skip Tasks, the gate in between.
	if err := s.made.TakeOver(ctx, who, screens.TakeOverArgs{
		ItemID: it.ID, Stage: string(item.StageImplementation),
	}); !errors.Is(err, item.ErrEscalationBypass) {
		t.Errorf("TakeOver past where it escalated = %v, want ErrEscalationBypass", err)
	}
	read, err := item.Get(ctx, d.pool, it.ID)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}
	if read.Stage != item.StageEscalated {
		t.Errorf("the item reads %s after a refused take-over, want it still escalated", read.Stage)
	}

	// Spec is above Tasks, and a human may still take the item over there.
	if err := s.made.TakeOver(ctx, who, screens.TakeOverArgs{
		ItemID: it.ID, Stage: string(item.StageSpec),
	}); err != nil {
		t.Fatalf("TakeOver to spec: %v", err)
	}
	read, err = item.Get(ctx, d.pool, it.ID)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}
	if read.Stage != item.StageSpec {
		t.Errorf("the item reads %s after the take-over, want spec", read.Stage)
	}
}
