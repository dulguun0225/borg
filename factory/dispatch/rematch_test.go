// The re-match: dispatch re-tests its open holds when a record able to clear
// one arrives, and writes the second row of every hold the match lifts. Each
// record the design names has an entry point of its own, called from the writer
// of that record, so a hold ends when its condition ends and not at whatever
// the next unrelated dispatch is. Split from hold_test.go by subject at the
// 500-line bound; these share db_test.go's fixtures and its package.
package dispatch_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/intent"
)

// TestRematchClosesAHoldWhoseConditionIsGone: dispatch re-matches its open
// holds when a record able to clear one arrives, and writes the second row of
// every hold the match lifts — so no hold outlives its condition.
func TestRematchClosesAHoldWhoseConditionIsGone(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{Text: aSpec}}, nil, 3)
	c.prompts.inForce = false
	it := c.oneItem(t, intent.StateRefined)
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, c.on(it), nil, agent.Refining{Statement: "s"}); !errors.Is(err, dispatch.ErrHeld) {
		t.Fatalf("SpecAuthor = %v, want ErrHeld", err)
	}

	c.prompts.inForce = true
	lifted, err := c.dispatch.Rematch(c.ctx)
	if err != nil {
		t.Fatalf("Rematch: %v", err)
	}
	if len(lifted) != 1 {
		t.Fatalf("%d holds lifted, want the one whose condition is gone", len(lifted))
	}
	open, _, err := c.dispatch.Open(c.ctx)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(open) != 0 {
		t.Errorf("%d holds still open, want none", len(open))
	}
}

// TestARolePromptEnteringForceLiftsItsOwnHoldsAndNoOthers: the gate a version
// fires putting one in force calls the re-match for that condition, so the
// stages whose role had no version in force move. Every other open hold is left
// for the record that ends it — a version entering force says nothing about an
// intent that has stopped.
func TestARolePromptEnteringForceLiftsItsOwnHoldsAndNoOthers(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{Text: aSpec}}, nil, 3)
	c.prompts.inForce = false
	waiting := c.oneItem(t, intent.StateRefined)
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, c.on(waiting), nil, agent.Refining{Statement: "s"}); !errors.Is(err, dispatch.ErrHeld) {
		t.Fatalf("the stage with no role prompt in force = %v, want ErrHeld", err)
	}
	c.prompts.inForce = true
	stopped := c.oneItem(t, intent.StateDropped)
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, c.on(stopped), nil, agent.Refining{Statement: "s"}); !errors.Is(err, dispatch.ErrHeld) {
		t.Fatalf("the stage whose intent stopped = %v, want ErrHeld", err)
	}

	lifted, err := c.dispatch.RematchOnRolePromptInForce(c.ctx)
	if err != nil {
		t.Fatalf("RematchOnRolePromptInForce: %v", err)
	}
	if len(lifted) != 1 {
		t.Fatalf("the version entering force lifted %d hold(s), want the one it ends", len(lifted))
	}
	open, _, err := c.dispatch.Open(c.ctx)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(open) != 1 || open[0].Condition != dispatch.HoldTheIntentStops {
		t.Fatalf("the holds still open are %+v, want the intent's own state standing alone", open)
	}
}

// TestAnIntentLeavingItsStopLiftsItsOwnHoldsAndNoOthers is the same wiring for
// the other record the design names: an intent leaving the state that stopped
// it re-matches that condition alone, and each hold re-reads its own intent, so
// one intent moving lifts nothing another is still stopped by.
func TestAnIntentLeavingItsStopLiftsItsOwnHoldsAndNoOthers(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{Text: aSpec}}, nil, 3)
	moving := c.oneItem(t, intent.StateDropped)
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, c.on(moving), nil, agent.Refining{Statement: "s"}); !errors.Is(err, dispatch.ErrHeld) {
		t.Fatalf("the stage whose intent was dropped = %v, want ErrHeld", err)
	}
	staying := c.oneItem(t, intent.StateEscalated)
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, c.on(staying), nil, agent.Refining{Statement: "s"}); !errors.Is(err, dispatch.ErrHeld) {
		t.Fatalf("the stage whose intent escalated = %v, want ErrHeld", err)
	}

	// The first intent leaves the state that stopped it, written the way every
	// other write to it is.
	if _, err := c.pool.Exec(c.ctx, `update `+intent.Table+` set state = $1 where id = $2`,
		string(intent.StateRefined), moving.IntentID); err != nil {
		t.Fatalf("refining the intent: %v", err)
	}

	lifted, err := c.dispatch.RematchOnIntentState(c.ctx)
	if err != nil {
		t.Fatalf("RematchOnIntentState: %v", err)
	}
	if len(lifted) != 1 {
		t.Fatalf("the intent leaving its stop lifted %d hold(s), want the one whose intent moved", len(lifted))
	}
	open, _, err := c.dispatch.Open(c.ctx)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(open) != 1 || open[0].ItemID != staying.ID {
		t.Fatalf("the holds still open are %+v, want the item whose intent is still escalated", open)
	}
}
