// The re-match dispatch runs when a record able to lift one of its holds is
// written, called from the writer of that record rather than waited for by the
// next unrelated dispatch. This file is that wiring: an intent leaving the
// state that stopped it. What each entry point re-tests is package dispatch's
// own, tested there.
package main

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/service"
)

// TestConfirmingTheReadingLiftsTheHoldTheIntentsStateOpened: a hold opened on
// an intent's state ends when the state does, and what says so is the write
// that ended it. Nothing dispatches between the confirming round and the read
// below, so a hold still standing is one waiting for an unrelated dispatch.
func TestConfirmingTheReadingLiftsTheHoldTheIntentsStateOpened(t *testing.T) {
	ctx, d, out := newPath(t, approvals)
	s := newScreens(t, ctx, d, out)
	svc, found, err := service.ByName(ctx, d.pool, theService)
	if err != nil || !found {
		t.Fatalf("ByName(%s) = found %v, %v", theService, found, err)
	}
	in, err := s.p.intake.TakeIn(ctx, s.p.human, intent.Arrival{
		Source: intent.SourceOwner, Statement: theService + ": a health endpoint", ProjectID: s.p.projectID,
	})
	if err != nil {
		t.Fatalf("taking the intent in: %v", err)
	}
	it, err := item.NewDecomposition(d.pool, d.token, item.NoHolds{}).Create(ctx, decompositionActor,
		item.New{IntentID: in.ID, ServiceID: svc.ID, AreaChain: []string{s.p.areaID}, Branch: "item/health", RequirementsAnswered: oneRequirement},
		s.p.projectID, s.p.projectID)
	if err != nil {
		t.Fatalf("decomposing the item: %v", err)
	}

	// An unrefined intent has no reading for the spec stage to author against,
	// which is one of the four states that stop a dispatch.
	onTheItem := dispatch.On{
		ItemID: it.ID, Stage: item.StageSpec, IntentID: in.ID,
		ProjectID: s.p.projectID, ServiceID: svc.ID, AreaID: s.p.areaID,
	}
	_, run, err := s.p.dispatch.SpecAuthor(ctx, onTheItem, nil, agent.Refining{Statement: in.Statement})
	if !errors.Is(err, dispatch.ErrHeld) || run.Held != dispatch.HoldTheIntentStops {
		t.Fatalf("the spec author on an unrefined intent = %v holding %q, want the intent's state holding", err, run.Held)
	}

	// The confirming round, written the way the pass writes it. It moves the
	// intent to refined, which is the intent leaving the state that stopped the
	// item, and the re-match runs from there.
	if _, err := s.p.intake.OpenRound(ctx, intakeActor, in.ID); err != nil {
		t.Fatalf("OpenRound: %v", err)
	}
	asked, err := s.p.intake.Ask(ctx, intakeActor, in.ID,
		confirmingQuestion([]string{"The system shall answer /healthz inside a second."}))
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	read, err := intent.Get(ctx, d.pool, in.ID)
	if err != nil {
		t.Fatalf("reading the intent: %v", err)
	}
	if err := s.p.confirmTheReading(ctx, s.p.human, read, asked); err != nil {
		t.Fatalf("confirming the reading: %v\n%s", err, out)
	}

	open, _, err := s.p.dispatch.Open(ctx)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for _, held := range open {
		if held.ItemID == it.ID && held.Condition == dispatch.HoldTheIntentStops {
			t.Fatalf("the hold on the intent's state still stands after the intent was refined: %+v", held)
		}
	}
}
