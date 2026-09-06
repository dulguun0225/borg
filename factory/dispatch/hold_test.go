// TestAnIntentThatStopsWorkIsAHoldAndNotARun and the rest of this file are
// dispatch's holds and its role/scope vocabulary, split from db_test.go by
// subject when that file passed 500 lines. They share db_test.go's fixtures
// and its package.
package dispatch_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
)

// TestAnIntentThatStopsWorkIsAHoldAndNotARun: dispatch reads the intent's
// state before it puts an agent on a stage, and four states stop it. The hold
// is a row of the log, no attempt is counted, and no agent worked.
func TestAnIntentThatStopsWorkIsAHoldAndNotARun(t *testing.T) {
	for _, state := range []intent.State{
		intent.StateUnrefined, intent.StateReDecomposing, intent.StateEscalated, intent.StateDropped,
	} {
		t.Run(string(state), func(t *testing.T) {
			c := newDispatch(t, []agent.Reply{{Text: aSpec}}, nil, 3)
			it := c.oneItem(t, state)

			_, run, err := c.dispatch.SpecAuthor(c.ctx, on(it), nil, agent.Refining{Statement: "s"})
			if !errors.Is(err, dispatch.ErrHeld) {
				t.Fatalf("SpecAuthor = %v, want ErrHeld", err)
			}
			if run.Held != dispatch.HoldTheIntentStops || run.HoldRow == "" {
				t.Errorf("the run held on %q as row %q, want the intent's state as a wait row", run.Held, run.HoldRow)
			}
			if c.model.calls != 0 {
				t.Error("an agent was put on the stage while the intent stopped work on it")
			}
			held, _, err := c.dispatch.Open(c.ctx)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if len(held) != 1 || held[0].ItemID != it.ID || held[0].Role != string(dispatch.RoleSpecAuthor) {
				t.Errorf("the open holds are %+v, want one naming this item and the role", held)
			}
		})
	}
}

// TestAStageNoEntryCoversAndARoleWithNoPromptAreHolds: two of the six
// conditions, each a row naming which condition held and the values the match
// was made on.
func TestAStageNoEntryCoversAndARoleWithNoPromptAreHolds(t *testing.T) {
	t.Run("no entry covers the stage", func(t *testing.T) {
		c := newDispatch(t, []agent.Reply{{Text: aSpec}}, nil, 3)
		c.fleet.covers = false
		it := c.oneItem(t, intent.StateRefined)

		_, run, err := c.dispatch.SpecAuthor(c.ctx, on(it), nil, agent.Refining{Statement: "s"})
		if !errors.Is(err, dispatch.ErrHeld) || run.Held != dispatch.HoldNoEntryCoversTheStage {
			t.Fatalf("SpecAuthor = %v holding %q, want the no-entry hold", err, run.Held)
		}
		held, _, err := c.dispatch.Open(c.ctx)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if len(held) != 1 || held[0].ServiceID != oneService {
			t.Errorf("the hold is %+v, want the values the match was made on", held)
		}
		// The row names the chain the scope was matched against and not one
		// area: a scope drawn anywhere on it would have covered the item.
		if len(held) == 1 && (len(held[0].AreaChain) != 2 ||
			held[0].AreaChain[0] != oneArea || held[0].AreaChain[1] != theAreaAbove) {
			t.Errorf("the hold names the area chain %v, want the item's own area and the one above it",
				held[0].AreaChain)
		}

		// A second dispatch of the same item and stage against the same
		// condition writes no second row: a hold is one row per item and stage.
		if _, run, err := c.dispatch.SpecAuthor(c.ctx, on(it), nil, agent.Refining{Statement: "s"}); !errors.Is(err, dispatch.ErrHeld) {
			t.Fatalf("the retry = %v, want ErrHeld", err)
		} else if run.HoldRow == "" {
			t.Error("the retry named no wait row, and the hold it met is one that stands")
		}
		again, rows, err := c.dispatch.Open(c.ctx)
		if err != nil {
			t.Fatalf("Open after the retry: %v", err)
		}
		if len(again) != 1 {
			t.Errorf("%d holds are open after the retry, want the one row per item and stage", len(rows))
		}
	})

	t.Run("no role prompt version in force", func(t *testing.T) {
		c := newDispatch(t, []agent.Reply{{Text: aSpec}}, nil, 3)
		c.prompts.inForce = false
		it := c.oneItem(t, intent.StateRefined)

		_, run, err := c.dispatch.SpecAuthor(c.ctx, on(it), nil, agent.Refining{Statement: "s"})
		if !errors.Is(err, dispatch.ErrHeld) || run.Held != dispatch.HoldNoRolePromptInForce {
			t.Fatalf("SpecAuthor = %v holding %q, want the no-prompt hold", err, run.Held)
		}
		if c.model.calls != 0 {
			t.Error("an agent was run with no role prompt version in force")
		}
	})
}

// TestRematchClosesAHoldWhoseConditionIsGone: dispatch re-matches its open
// holds when a record able to clear one arrives, and writes the second row of
// every hold the match lifts — so no hold outlives its condition.
func TestRematchClosesAHoldWhoseConditionIsGone(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{Text: aSpec}}, nil, 3)
	c.prompts.inForce = false
	it := c.oneItem(t, intent.StateRefined)
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, on(it), nil, agent.Refining{Statement: "s"}); !errors.Is(err, dispatch.ErrHeld) {
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

// TestTheFourRolesMatchTheFourAuthoringStages: dispatch is a match of the
// item's stage against the role, so every stage an artifact is authored at has
// exactly one role and no other stage has any.
func TestTheFourRolesMatchTheFourAuthoringStages(t *testing.T) {
	for _, stage := range item.AuthoringStages {
		role, found := dispatch.RoleAt(stage)
		if !found {
			t.Fatalf("no role is put on %s", stage)
		}
		at, err := role.Stage()
		if err != nil || at != stage {
			t.Errorf("%s names stage %s (%v), want %s", role, at, err, stage)
		}
	}
	for _, stage := range []item.Stage{item.StageQueued, item.StageMerged, item.StageDropped} {
		if role, found := dispatch.RoleAt(stage); found {
			t.Errorf("%s is put on %s, and nothing authors there", role, stage)
		}
	}
}

// TestAnEntryNarrowsARolesOperationsAndNeverWidensThem: the factory defines
// the operation list per role, an owner may leave one out, and adding one is
// refused.
func TestAnEntryNarrowsARolesOperationsAndNeverWidensThem(t *testing.T) {
	full, err := dispatch.RoleImplementer.Operations()
	if err != nil {
		t.Fatalf("Operations: %v", err)
	}
	narrowed, err := dispatch.RoleImplementer.Narrow(full[:1])
	if err != nil || len(narrowed) != 1 {
		t.Errorf("Narrow to one operation = %v, %v, want the one", narrowed, err)
	}
	if _, err := dispatch.RoleSpecAuthor.Narrow([]string{dispatch.OperationWriteTheRepository}); !errors.Is(err, dispatch.ErrOperationWidened) {
		t.Errorf("widening the spec author's list = %v, want ErrOperationWidened", err)
	}
}

// TestAScopeBindsWhatAnEntryMayBePutOn: a scope is drawn on a project, a
// service and an area, and a field it leaves empty matches whatever the item
// has.
func TestAScopeBindsWhatAnEntryMayBePutOn(t *testing.T) {
	item := dispatch.On{ProjectID: oneProject, ServiceID: oneService, AreaID: oneArea}
	if !(dispatch.Scope{}).Covers(item) {
		t.Error("the empty scope covers nothing, and it is the whole factory")
	}
	if !(dispatch.Scope{ProjectID: oneProject}).Covers(item) {
		t.Error("a project-wide scope does not cover an item in the project")
	}
	if (dispatch.Scope{AreaID: "ar_" + strings.Repeat("2", 32)}).Covers(item) {
		t.Error("an area-scoped entry covers an item in another area")
	}
	// Both halves of a scope: an entry drawn on an area above the item's
	// reaches it, so declaring a finer area never takes the item out of it.
	inChain := dispatch.On{
		ProjectID: oneProject, ServiceID: oneService,
		AreaID: oneArea, AreaChain: []string{oneArea, theAreaAbove},
	}
	if !(dispatch.Scope{AreaID: theAreaAbove}).Covers(inChain) {
		t.Error("an entry drawn on an area above the item's does not cover it, and the chain is what a scope is matched against")
	}
	if (dispatch.Scope{AreaID: theAreaAbove}).Covers(item) {
		t.Error("an item whose caller supplied no chain is covered by an area it does not name")
	}
	if got := (dispatch.Scope{ServiceID: oneService}).String(); !strings.Contains(got, oneService) {
		t.Errorf("the scope reads as %q, want it to name the service the principal carries", got)
	}
}

// TestAStageEnteredAgainAfterARejectCountsAndEscalatesAtTheLimit: a reject
// sends the item back to the stage to be entered again, and the entry is what
// counts — so an item sent back for the last time its limit allows escalates
// before an agent is put on it again.
func TestAStageEnteredAgainAfterARejectCountsAndEscalatesAtTheLimit(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{Text: aSpec}}, nil, 2)
	it := c.oneItem(t, intent.StateRefined)
	returned := on(it)
	returned.Reentering = true

	// The first re-entry is the second attempt, which the limit allows.
	if _, run, err := c.dispatch.SpecAuthor(c.ctx, returned, nil, agent.Refining{Statement: "s"}); err != nil {
		t.Fatalf("SpecAuthor after a reject: %v", err)
	} else if run.Attempts != 2 {
		t.Errorf("the run stood at %d attempts, want the second entry", run.Attempts)
	}

	// The second re-entry is the third, which it does not.
	_, run, err := c.dispatch.SpecAuthor(c.ctx, returned, nil, agent.Refining{Statement: "s"})
	if !errors.Is(err, dispatch.ErrOutOfAttempts) {
		t.Fatalf("SpecAuthor = %v, want ErrOutOfAttempts", err)
	}
	if !run.Escalated || len(c.escalation.items) != 1 {
		t.Errorf("the run escalated %v and the escalation saw %v, want the item escalated once", run.Escalated, c.escalation.items)
	}
	// The wait an escalation leaves is dispatch's own call, made after the item
	// was written escalated and its pending rows abandoned.
	if len(c.told.items) != 1 || c.told.items[0] != it.ID ||
		c.told.reasons[0] != dispatch.EscalatedByTheAttemptLimit {
		t.Errorf("the notifier was told %v for %v, want this item at the attempt limit", c.told.reasons, c.told.items)
	}
	if c.model.calls != 1 {
		t.Errorf("%d calls, want no agent put on the stage after the limit was spent", c.model.calls)
	}
}
