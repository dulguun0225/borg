// The interview as a stage of the pass: a round nothing answers waits in Work,
// and an empty answer is refused rather than written.
package main

import (
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/screens"
)

// TestARoundNothingAnswersWaitsInWork composes a run with no answer at all,
// which is what every composition that serves the screens is: the interviewer
// asks its question, the round waits in Work, and nothing below it is
// decomposed — an install with no screen open is not stuck but shown as stuck.
func TestARoundNothingAnswersWaitsInWork(t *testing.T) {
	ctx, d, out := newPath(t, "\n"+approvals)

	res, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	if !strings.Contains(out.String(), "waits in Work on a round of the interview") {
		t.Errorf("the run does not report the round waiting in Work:\n%s", out)
	}
	if len(res.candidates) != 0 {
		t.Errorf("the run decomposed %d item(s) over an interview nobody answered", len(res.candidates))
	}
	if len(res.decompositions) != 1 {
		t.Fatalf("the run took %d intent(s) in, want the one it was given", len(res.decompositions))
	}
	intentID := res.decompositions[0].intentID

	// The one round waiting, and the answer that ends it. An empty answer is
	// refused: the answer is write-once and a blank one would stamp the
	// question answered with nothing in it.
	questions, err := intent.Questions(ctx, d.pool, intentID)
	if err != nil {
		t.Fatalf("reading the questions: %v", err)
	}
	if len(questions) != 1 || questions[0].Answered() {
		t.Fatalf("the intent has %+v, want the interviewer's one question unanswered", questions)
	}

	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path: %v\n%s", err, out)
	}
	made := &calls{p: p, v: &views{p: p}}
	who := principal.OfHuman(owner(t, ctx, d.pool, d.token, d.human).Key, record.BasisClaimed)
	if err := made.AnswerQuestion(ctx, who, screens.AnswerQuestionArgs{
		QuestionID: questions[0].ID, Answer: "   ",
	}); err == nil {
		t.Error("a blank answer was written, and the answer is what the round is spent on")
	}
	if err := made.AnswerQuestion(ctx, who, screens.AnswerQuestionArgs{
		QuestionID: questions[0].ID, Answer: theAnswer,
	}); err != nil {
		t.Fatalf("answering the round at Work: %v\n%s", err, out)
	}

	// The next pass continues from the answer: the confirming round is asked,
	// and it waits in Work too.
	if _, err := p.advance(ctx); err != nil {
		t.Fatalf("the pass after the answer: %v\n%s", err, out)
	}
	questions, err = intent.Questions(ctx, d.pool, intentID)
	if err != nil {
		t.Fatalf("reading the questions: %v", err)
	}
	if len(questions) != 2 || !questions[0].Answered() {
		t.Fatalf("the intent has %+v, want the answered question and the confirming round after it", questions)
	}
	if readingIn(questions[1].Question) == nil {
		t.Errorf("the second question is %q, want the confirming round stating the reading", questions[1].Question)
	}
}

// TestTheConfirmingRoundIsClosedAtWork is the other half: the requester
// confirms the reading at Work, which writes the requirement set and refines
// the intent, and the pass after it decomposes.
func TestTheConfirmingRoundIsClosedAtWork(t *testing.T) {
	ctx, d, out := newPath(t, "\n"+approvals)
	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path: %v\n%s", err, out)
	}
	in, err := p.take(ctx, theStatement)
	if err != nil {
		t.Fatalf("taking the intent in: %v\n%s", err, out)
	}
	p.servicesOf[in.ID] = []string{theService}

	made := &calls{p: p, v: &views{p: p}}
	who := principal.OfHuman(owner(t, ctx, d.pool, d.token, d.human).Key, record.BasisClaimed)
	for round := 1; round <= 2; round++ {
		if _, err := p.advance(ctx); err != nil {
			t.Fatalf("pass %d: %v\n%s", round, err, out)
		}
		waiting, found, err := unansweredRound(ctx, d.pool, in.ID)
		if err != nil {
			t.Fatalf("reading the round waiting: %v", err)
		}
		if !found {
			t.Fatalf("pass %d left no round waiting:\n%s", round, out)
		}
		if readingIn(waiting.Question) != nil {
			if err := made.ConfirmReading(ctx, who, screens.ConfirmReadingArgs{
				IntentID: in.ID, Confirmed: true,
			}); err != nil {
				t.Fatalf("confirming the reading at Work: %v\n%s", err, out)
			}
			continue
		}
		if err := made.AnswerQuestion(ctx, who, screens.AnswerQuestionArgs{
			QuestionID: waiting.ID, Answer: theAnswer,
		}); err != nil {
			t.Fatalf("answering the round at Work: %v\n%s", err, out)
		}
	}

	refined, err := intent.Get(ctx, d.pool, in.ID)
	if err != nil {
		t.Fatalf("reading the intent: %v", err)
	}
	if refined.State != intent.StateRefined {
		t.Fatalf("the intent is %s after the reading was confirmed, want refined:\n%s", refined.State, out)
	}
	requirements, err := intent.Requirements(ctx, d.pool, in.ID)
	if err != nil {
		t.Fatalf("reading the requirements: %v", err)
	}
	if len(requirements) == 0 {
		t.Error("the confirming round wrote no requirement, and the reading it stated is the set")
	}
}
