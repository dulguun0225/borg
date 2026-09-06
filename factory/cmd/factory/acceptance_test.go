// The round that follows production: the run asks it once every item of the
// intent is live, and the answer is what delivers the intent.
package main

import (
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/intent"
)

// TestTheAcceptanceRoundIsAskedAndDeliversTheIntent: the run takes one item to
// production, asks the round that follows it, and leaves the intent refined
// with the question unanswered — the wait on a requester the design says is
// unbounded and spends nothing. Answering it is what writes delivered and the
// outcome, which is what `factory accept` performs at the terminal.
func TestTheAcceptanceRoundIsAskedAndDeliversTheIntent(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)

	res, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	intentID := only(t, res).intentID
	if !strings.Contains(out.String(), "Acceptance round") {
		t.Errorf("the run asked no acceptance round:\n%s", out)
	}

	asked, err := outstandingRound(ctx, d.pool, intentID)
	if err != nil {
		t.Fatalf("reading the round waiting on an answer: %v", err)
	}
	if !strings.Contains(asked.Question, theStatement) {
		t.Errorf("the round asks %q, want what was asked for in it", asked.Question)
	}
	read, err := intent.Get(ctx, d.pool, intentID)
	if err != nil {
		t.Fatalf("reading the intent: %v", err)
	}
	if read.State != intent.StateRefined || read.Outcome != "" {
		t.Errorf("before the answer the intent is %s with outcome %q, want refined with none",
			read.State, read.Outcome)
	}

	// The answering half, which at Work is a screen and here is the write that
	// subcommand makes.
	human, err := humanNamed(ctx, d.pool, d.token, "owner")
	if err != nil {
		t.Fatalf("resolving the human: %v", err)
	}
	const verdict = "the effect was had"
	if err := intent.NewIntake(d.pool, d.token, intent.NoNotifier{}).Delivered(ctx, human, intent.Delivery{
		IntentID: intentID, QuestionID: asked.ID, Answer: verdict, Outcome: verdict,
	}); err != nil {
		t.Fatalf("Delivered: %v", err)
	}
	read, err = intent.Get(ctx, d.pool, intentID)
	if err != nil {
		t.Fatalf("reading the intent after the answer: %v", err)
	}
	if read.State != intent.StateDelivered || read.Outcome != verdict {
		t.Errorf("the intent is %s with outcome %q, want delivered with the verdict on the intended effect",
			read.State, read.Outcome)
	}
}
