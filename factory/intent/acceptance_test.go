package intent_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/intent"
)

// TestAcceptanceRoundAsksAndCounts: the round that follows production is a
// question record like any other, and it costs a round of its own, because
// the count is what the attempt limit reads.
func TestAcceptanceRoundAsksAndCounts(t *testing.T) {
	ctx, pool, in := newIntake(t)
	taken := requested(t, ctx, in, "checkout should retry")

	if _, err := in.AcceptanceRound(ctx, intake, taken.ID, "Did the retry fix it?"); !errors.Is(err, intent.ErrNotRefined) {
		t.Errorf("AcceptanceRound on an unrefined intent = %v, want ErrNotRefined", err)
	}

	intentID := confirmed(t, ctx, in, "checkout should retry",
		intent.NewRequirement{Statement: "The system shall retry a failed charge."},
	)
	asked, err := in.AcceptanceRound(ctx, intake, intentID, "Did the retry fix it?")
	if err != nil {
		t.Fatalf("AcceptanceRound: %v", err)
	}
	read, err := intent.Get(ctx, pool, intentID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Rounds != 2 {
		t.Errorf("the acceptance round left %d rounds, want 2 — the confirming round and this one", read.Rounds)
	}
	if asked.Round != 2 {
		t.Errorf("the acceptance question is at round %d, want 2", asked.Round)
	}

	own := raised(t, ctx, in, crossing, "Revert release 9 of checkout.")
	confirmEnumerated(t, ctx, in, own.ID)
	if _, err := in.AcceptanceRound(ctx, intake, own.ID, "anything"); !errors.Is(err, intent.ErrNoRequester) {
		t.Errorf("AcceptanceRound on the factory's own = %v, want ErrNoRequester", err)
	}
	if _, err := in.AcceptanceRound(ctx, intake, intentID, ""); !errors.Is(err, intent.ErrQuestionEmpty) {
		t.Errorf("AcceptanceRound with no question = %v, want ErrQuestionEmpty", err)
	}
}

// TestDeliveredWritesTheOutcome: delivered is the verdict on the intended
// effect and not on the reading, and the outcome is computed once, at the
// close.
func TestDeliveredWritesTheOutcome(t *testing.T) {
	ctx, pool, in := newIntake(t)
	intentID := confirmed(t, ctx, in, "checkout should retry",
		intent.NewRequirement{Statement: "The system shall retry a failed charge."},
	)
	asked, err := in.AcceptanceRound(ctx, intake, intentID, "Did the retry fix it?")
	if err != nil {
		t.Fatalf("AcceptanceRound: %v", err)
	}

	if err := in.Delivered(ctx, intake, intent.Delivery{IntentID: intentID}); !errors.Is(err, intent.ErrRequesterOwed) {
		t.Errorf("Delivered with no acceptance question = %v, want ErrRequesterOwed", err)
	}
	if err := in.Delivered(ctx, intake, intent.Delivery{
		IntentID: intentID, QuestionID: asked.ID, Answer: "Yes.",
	}); !errors.Is(err, intent.ErrOutcomeEmpty) {
		t.Errorf("Delivered with no outcome = %v, want ErrOutcomeEmpty", err)
	}

	if err := in.Delivered(ctx, intake, intent.Delivery{
		IntentID: intentID, QuestionID: asked.ID, Answer: "Yes, it did.", Outcome: "the effect was had",
	}); err != nil {
		t.Fatalf("Delivered: %v", err)
	}
	read, err := intent.Get(ctx, pool, intentID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.State != intent.StateDelivered || read.Outcome != "the effect was had" {
		t.Errorf("the intent is %s with outcome %q, want delivered with the outcome given", read.State, read.Outcome)
	}
	questions, err := intent.Questions(ctx, pool, intentID)
	if err != nil {
		t.Fatalf("Questions: %v", err)
	}
	if len(questions) != 2 || !questions[1].Answered() {
		t.Errorf("Questions = %+v, want the acceptance question answered", questions)
	}

	if err := in.Delivered(ctx, intake, intent.Delivery{IntentID: intentID}); !errors.Is(err, intent.ErrNotRefined) {
		t.Errorf("Delivered on an already delivered intent = %v, want ErrNotRefined", err)
	}
}

// TestCorrectAcceptanceSendsBackAndCountsTheRound: a correction at the
// acceptance round attaches like any other answer and reopens the interview
// the way a replacement constraint's raise does. The round it costs was
// already counted when the acceptance round was asked.
func TestCorrectAcceptanceSendsBackAndCountsTheRound(t *testing.T) {
	ctx, pool, in := newIntake(t)
	intentID := confirmed(t, ctx, in, "checkout should retry",
		intent.NewRequirement{Statement: "The system shall retry a failed charge."},
	)
	asked, err := in.AcceptanceRound(ctx, intake, intentID, "Did the retry fix it?")
	if err != nil {
		t.Fatalf("AcceptanceRound: %v", err)
	}

	if err := in.CorrectAcceptance(ctx, intake, intentID, asked.ID, ""); !errors.Is(err, intent.ErrAnswerEmpty) {
		t.Errorf("CorrectAcceptance with no correction = %v, want ErrAnswerEmpty", err)
	}
	if err := in.CorrectAcceptance(ctx, intake, intentID, asked.ID, "No, it still fails."); err != nil {
		t.Fatalf("CorrectAcceptance: %v", err)
	}
	read, err := intent.Get(ctx, pool, intentID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.State != intent.StateUnrefined || read.SentBackBy != intent.SentBackByAcceptanceCorrection || read.Rounds != 2 {
		t.Errorf("after the correction the intent is %s sent back by %s at %d rounds, want unrefined by acceptance_correction at 2",
			read.State, read.SentBackBy, read.Rounds)
	}

	own := raised(t, ctx, in, crossing, "Revert release 9 of checkout.")
	confirmEnumerated(t, ctx, in, own.ID)
	if err := in.CorrectAcceptance(ctx, intake, own.ID, asked.ID, "no"); !errors.Is(err, intent.ErrNoRequester) {
		t.Errorf("CorrectAcceptance on the factory's own = %v, want ErrNoRequester", err)
	}
}
