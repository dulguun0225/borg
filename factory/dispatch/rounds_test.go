// What the attempt limit is compared against: the item's own count for the
// stage, which rises as the item is entered again, and the rounds the intent's
// own record keeps for a role put on an intent. Split from db_test.go by
// subject at the 500-line bound; these share its fixtures and its package.
package dispatch_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
)

// aReading is a reply the interviewer's protocol accepts as a reading.
const aReading = "READING:\nREQUIREMENT: The system shall answer /healthz inside a second."

// TestTheRoundsTheIntentKeepsAreWhatTheLimitCompares: how many rounds have been
// asked is a field of the intent, written by intake at each round, and it is
// what the limit is compared against — not a number the caller carries beside
// it. A caller that carries nothing still reaches the limit.
func TestTheRoundsTheIntentKeepsAreWhatTheLimitCompares(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{Text: aReading}}, nil, 2)
	in, err := c.intake.TakeIn(c.ctx, owner,
		intent.Arrival{Source: intent.SourceOwner, Statement: "a health endpoint", ProjectID: oneProject})
	if err != nil {
		t.Fatalf("TakeIn: %v", err)
	}
	// Three rounds asked against a limit of two, written the way intake writes
	// them: the intent has spent more than the limit allows.
	for range 3 {
		if _, err := c.intake.OpenRound(c.ctx, owner, in.ID); err != nil {
			t.Fatalf("OpenRound: %v", err)
		}
	}

	_, run, err := c.dispatch.Interviewer(c.ctx,
		dispatch.On{IntentID: in.ID, ProjectID: oneProject}, nil, agent.Interviewing{Statement: "s"})
	if !errors.Is(err, dispatch.ErrOutOfAttempts) {
		t.Fatalf("Interviewer on an intent three rounds in = %v, want ErrOutOfAttempts", err)
	}
	if run.Attempts != 3 {
		t.Errorf("the run counted %d, want the three rounds the intent's own record keeps", run.Attempts)
	}
	if c.model.calls != 0 {
		t.Error("a round ran after the intent had spent its limit")
	}
}

// TestARoundInsideTheLimitRuns: the same read one round below the limit puts
// an agent on the intent, so the comparison is against the field and not
// against the presence of any rounds at all.
func TestARoundInsideTheLimitRuns(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{Text: aReading}}, nil, 2)
	in, err := c.intake.TakeIn(c.ctx, owner,
		intent.Arrival{Source: intent.SourceOwner, Statement: "a health endpoint", ProjectID: oneProject})
	if err != nil {
		t.Fatalf("TakeIn: %v", err)
	}
	if _, err := c.intake.OpenRound(c.ctx, owner, in.ID); err != nil {
		t.Fatalf("OpenRound: %v", err)
	}

	read, run, err := c.dispatch.Interviewer(c.ctx,
		dispatch.On{IntentID: in.ID, ProjectID: oneProject}, nil, agent.Interviewing{Statement: "s"})
	if err != nil {
		t.Fatalf("Interviewer: %v", err)
	}
	if len(read.Requirements) == 0 {
		t.Fatalf("the interviewer read %+v, want the reading the reply carried", read)
	}
	if run.Attempts != 1 {
		t.Errorf("the run counted %d, want the one round the intent's record keeps", run.Attempts)
	}
}

// TestARefusedReplyIsEnteredAgainAndCountedOnTheItem: a second attempt at one
// stage is the item entering it again, so the count rises on the record rather
// than in the process — which is what makes a second run of the factory carry
// on from what the first spent.
func TestARefusedReplyIsEnteredAgainAndCountedOnTheItem(t *testing.T) {
	c := newDispatch(t, []agent.Reply{
		{Text: "not the protocol", Units: map[string]int64{agent.UnitsOutput: 1}},
		{Text: aSpec, Units: map[string]int64{agent.UnitsOutput: 2}},
	}, nil, 3)
	it := c.oneItem(t, intent.StateRefined)

	_, run, err := c.dispatch.SpecAuthor(c.ctx, c.on(it), nil, agent.Refining{Statement: "s"})
	if err != nil {
		t.Fatalf("SpecAuthor: %v", err)
	}
	if len(run.AgentRunIDs) != 2 {
		t.Errorf("%d run records, want one per call including the refused one", len(run.AgentRunIDs))
	}
	stages, err := item.Stages(c.ctx, c.pool, it.ID)
	if err != nil {
		t.Fatalf("Stages: %v", err)
	}
	if len(stages) != 1 || stages[0].Attempts != 2 {
		t.Fatalf("the item stands at %+v, want two attempts at spec", stages)
	}
}

// TestTheStoredCountIsWhatTheLimitIsComparedAgainst: a dispatch onto an item
// that has already spent its allowance escalates on its first refused reply,
// because the count it reads is the item's own and not this call's.
func TestTheStoredCountIsWhatTheLimitIsComparedAgainst(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{Text: "not the protocol"}}, nil, 2)
	it := c.oneItem(t, intent.StateRefined)
	// The item has been entered once by decomposition; two more entries put its
	// count above the limit, which is what exceeding one is.
	for range 2 {
		if _, err := c.items.Enter(c.ctx, dispatch.Actor, it.ID, item.StageSpec); err != nil {
			t.Fatalf("Enter: %v", err)
		}
	}

	_, run, err := c.dispatch.SpecAuthor(c.ctx, c.on(it), nil, agent.Refining{Statement: "s"})
	if !errors.Is(err, dispatch.ErrOutOfAttempts) {
		t.Fatalf("SpecAuthor = %v, want ErrOutOfAttempts", err)
	}
	if !run.Escalated {
		t.Error("the run does not say the item escalated")
	}
	if len(c.escalation.items) != 1 || c.escalation.items[0] != it.ID {
		t.Errorf("escalated %v, want this item", c.escalation.items)
	}
	if c.model.calls != 0 {
		t.Errorf("%d calls, want no agent put on a stage whose count already exceeds the limit", c.model.calls)
	}
}
