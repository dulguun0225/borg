package intent_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/intent"
)

// TestSendBackReopensTheInterview: sending back writes unrefined again and
// names what sent it, which is what reopens the interview; an intent already
// dropped or delivered is refused, both being ends.
func TestSendBackReopensTheInterview(t *testing.T) {
	ctx, pool, in := newIntake(t)
	intentID := confirmed(t, ctx, in, "checkout should retry",
		intent.NewRequirement{Statement: "The system shall retry a failed charge."},
	)

	if err := in.SendBack(ctx, intake, intentID, intent.SentBackByGateReject); err != nil {
		t.Fatalf("SendBack: %v", err)
	}
	read, err := intent.Get(ctx, pool, intentID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.State != intent.StateUnrefined || read.SentBackBy != intent.SentBackByGateReject {
		t.Errorf("after SendBack the intent is %s sent back by %s, want unrefined by gate_reject", read.State, read.SentBackBy)
	}

	if err := in.SendBack(ctx, intake, intentID, "a wish"); !errors.Is(err, intent.ErrSentBackByUnknown) {
		t.Errorf("SendBack with an unknown cause = %v, want ErrSentBackByUnknown", err)
	}

	if err := in.Drop(ctx, owner, intentID); err != nil {
		t.Fatalf("Drop: %v", err)
	}
	if err := in.SendBack(ctx, intake, intentID, intent.SentBackByReworkRequest); !errors.Is(err, intent.ErrFinished) {
		t.Errorf("SendBack on a dropped intent = %v, want ErrFinished", err)
	}
}

// TestReDecompositionTracksItsOwnCount: the re-decomposition count is a field
// of its own beside the rounds, advanced at the open and never at the close,
// so an interview's rounds are never spent out of decomposition's budget.
func TestReDecompositionTracksItsOwnCount(t *testing.T) {
	ctx, pool, in := newIntake(t)
	intentID := confirmed(t, ctx, in, "checkout should retry",
		intent.NewRequirement{Statement: "The system shall retry a failed charge."},
	)

	reached, err := in.MarkReDecomposing(ctx, intake, intentID)
	if err != nil {
		t.Fatalf("MarkReDecomposing: %v", err)
	}
	if reached != 1 {
		t.Errorf("the first re-decomposition reached %d, want 1", reached)
	}
	read, err := intent.Get(ctx, pool, intentID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.State != intent.StateReDecomposing || read.ReDecompositions != 1 || read.Rounds != 1 {
		t.Errorf("after the open the intent is %s at %d re-decompositions and %d rounds, want re_decomposing, 1, 1",
			read.State, read.ReDecompositions, read.Rounds)
	}

	if _, err := in.MarkReDecomposing(ctx, intake, intentID); !errors.Is(err, intent.ErrNotRefined) {
		t.Errorf("MarkReDecomposing while already re-decomposing = %v, want ErrNotRefined", err)
	}
	if err := in.ClearReDecomposing(ctx, intake, intentID); err != nil {
		t.Fatalf("ClearReDecomposing: %v", err)
	}
	read, err = intent.Get(ctx, pool, intentID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.State != intent.StateRefined || read.ReDecompositions != 1 {
		t.Errorf("after the close the intent is %s at %d re-decompositions, want refined, 1", read.State, read.ReDecompositions)
	}
	if err := in.ClearReDecomposing(ctx, intake, intentID); !errors.Is(err, intent.ErrNotReDecomposing) {
		t.Errorf("ClearReDecomposing while refined = %v, want ErrNotReDecomposing", err)
	}

	reached, err = in.MarkReDecomposing(ctx, intake, intentID)
	if err != nil {
		t.Fatalf("MarkReDecomposing again: %v", err)
	}
	if reached != 2 {
		t.Errorf("the second re-decomposition reached %d, want 2", reached)
	}
}

// TestEscalateWritesEscalatedWhenACountExceedsTheLimit: the limit is the
// caller's own argument and is written rather than recomputed, because a
// decision read back against a value no longer in force is not the decision
// that was taken.
func TestEscalateWritesEscalatedWhenACountExceedsTheLimit(t *testing.T) {
	ctx, pool, in := newIntake(t)
	taken := requested(t, ctx, in, "checkout should retry")

	if _, err := in.Escalate(ctx, intake, taken.ID, 0); !errors.Is(err, intent.ErrLimitNotPositive) {
		t.Errorf("Escalate with a limit of 0 = %v, want ErrLimitNotPositive", err)
	}
	if _, err := in.Escalate(ctx, intake, taken.ID, 2); !errors.Is(err, intent.ErrLimitNotExceeded) {
		t.Errorf("Escalate before either count exceeds the limit = %v, want ErrLimitNotExceeded", err)
	}

	for range 3 {
		if _, err := in.OpenRound(ctx, intake, taken.ID); err != nil {
			t.Fatalf("OpenRound: %v", err)
		}
	}
	escalated, err := in.Escalate(ctx, intake, taken.ID, 2)
	if err != nil {
		t.Fatalf("Escalate: %v", err)
	}
	if escalated.State != intent.StateEscalated {
		t.Errorf("Escalate returned %+v, want escalated", escalated)
	}
	read, err := intent.Get(ctx, pool, taken.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.State != intent.StateEscalated {
		t.Errorf("the intent is %s, want escalated", read.State)
	}

	if err := in.Drop(ctx, owner, taken.ID); err != nil {
		t.Fatalf("Drop: %v", err)
	}
	if _, err := in.Escalate(ctx, intake, taken.ID, 1); !errors.Is(err, intent.ErrFinished) {
		t.Errorf("Escalate on a dropped intent = %v, want ErrFinished", err)
	}
}

// TestDropEndsAnIntentForGood: a human at Work ends an intent for good, and a
// component is refused, because an end read from a component is not the
// decision this value records.
func TestDropEndsAnIntentForGood(t *testing.T) {
	ctx, pool, in := newIntake(t)
	taken := requested(t, ctx, in, "checkout should retry")

	if err := in.Drop(ctx, intake, taken.ID); !errors.Is(err, intent.ErrNotAHuman) {
		t.Errorf("Drop by a component = %v, want ErrNotAHuman", err)
	}
	if err := in.Drop(ctx, owner, taken.ID); err != nil {
		t.Fatalf("Drop: %v", err)
	}
	read, err := intent.Get(ctx, pool, taken.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.State != intent.StateDropped {
		t.Errorf("the intent is %s, want dropped", read.State)
	}
	if err := in.Drop(ctx, owner, taken.ID); !errors.Is(err, intent.ErrFinished) {
		t.Errorf("Drop on an already dropped intent = %v, want ErrFinished", err)
	}
	if err := in.Drop(ctx, owner, "in_missing"); !errors.Is(err, intent.ErrIntentNotFound) {
		t.Errorf("Drop on a missing intent = %v, want ErrIntentNotFound", err)
	}
}

// TestAHumansAnswerClearsAnEscalatedInterview: the escalation is the factory
// saying it cannot refine this one, and a human who answers clears it and
// starts the round count again — the decision to spend more. A component's
// answer clears nothing, and the re-decomposition count is left where it is.
func TestAHumansAnswerClearsAnEscalatedInterview(t *testing.T) {
	ctx, pool, in := newIntake(t)
	taken := requested(t, ctx, in, "checkout should retry")

	var asked []intent.Question
	for range 3 {
		if _, err := in.OpenRound(ctx, intake, taken.ID); err != nil {
			t.Fatalf("OpenRound: %v", err)
		}
		question, err := in.Ask(ctx, intake, taken.ID, "Against which provider?")
		if err != nil {
			t.Fatalf("Ask: %v", err)
		}
		asked = append(asked, question)
	}
	if _, err := in.Escalate(ctx, intake, taken.ID, 2); err != nil {
		t.Fatalf("Escalate: %v", err)
	}

	// A component answering is the factory answering itself, which is not the
	// decision the clearing records.
	if _, err := in.Answer(ctx, intake, asked[0].ID, "the same provider"); err != nil {
		t.Fatalf("Answer as a component: %v", err)
	}
	read, err := intent.Get(ctx, pool, taken.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.State != intent.StateEscalated || read.Rounds != 3 {
		t.Errorf("after a component's answer the intent is %s at %d rounds, want escalated at 3",
			read.State, read.Rounds)
	}

	if _, err := in.Answer(ctx, owner, asked[1].ID, "once per charge"); err != nil {
		t.Fatalf("Answer as a human: %v", err)
	}
	read, err = intent.Get(ctx, pool, taken.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.State != intent.StateUnrefined || read.Rounds != 0 {
		t.Errorf("after a human's answer the intent is %s at %d rounds, want unrefined at 0",
			read.State, read.Rounds)
	}
	if read.ReDecompositions != 0 {
		t.Errorf("the clearing moved the re-decomposition count to %d, want it left alone", read.ReDecompositions)
	}

	// The interview goes on from zero: the round count starts again, so the
	// same limit is compared against the rounds asked since the clearing.
	round, err := in.OpenRound(ctx, intake, taken.ID)
	if err != nil {
		t.Fatalf("OpenRound after the clearing: %v", err)
	}
	if round != 1 {
		t.Errorf("the first round after the clearing is %d, want 1", round)
	}

	// A human's answer on an intent that is not escalated moves no state.
	if _, err := in.Answer(ctx, owner, asked[2].ID, "a day"); err != nil {
		t.Fatalf("Answer on an unrefined intent: %v", err)
	}
	read, err = intent.Get(ctx, pool, taken.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.State != intent.StateUnrefined || read.Rounds != 1 {
		t.Errorf("an answer on an unrefined intent left it %s at %d rounds, want unrefined at 1",
			read.State, read.Rounds)
	}
}

// TestTheProjectIsFilledOnceAndNeverRewritten: intake fills the project where
// decomposition has to place a created service and nothing else answers, and
// the field is never rewritten, so an approval keeps pointing at what was
// approved.
func TestTheProjectIsFilledOnceAndNeverRewritten(t *testing.T) {
	ctx, pool, in := newIntake(t)
	taken := requested(t, ctx, in, "a shop needs a basket service")
	if taken.ProjectID != "" {
		t.Fatalf("the intent arrived naming project %q, want none", taken.ProjectID)
	}

	if err := in.SetProject(ctx, intake, taken.ID, ""); !errors.Is(err, intent.ErrProjectIDEmpty) {
		t.Errorf("SetProject naming no project = %v, want ErrProjectIDEmpty", err)
	}
	if err := in.SetProject(ctx, intake, taken.ID, "pr_shop"); err != nil {
		t.Fatalf("SetProject: %v", err)
	}
	read, err := intent.Get(ctx, pool, taken.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.ProjectID != "pr_shop" {
		t.Errorf("the intent names project %q, want pr_shop", read.ProjectID)
	}

	for _, again := range []string{"pr_shop", "pr_other"} {
		if err := in.SetProject(ctx, intake, taken.ID, again); !errors.Is(err, intent.ErrProjectAlreadyWritten) {
			t.Errorf("SetProject a second time with %s = %v, want ErrProjectAlreadyWritten", again, err)
		}
	}
	read, err = intent.Get(ctx, pool, taken.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.ProjectID != "pr_shop" {
		t.Errorf("after the refused rewrites the intent names %q, want pr_shop", read.ProjectID)
	}

	// A project supplied at the arrival is written already, so decomposition's
	// fill is refused there too.
	supplied, err := in.TakeIn(ctx, owner, intent.Arrival{
		Source: intent.SourceOwner, Statement: "another shop", ProjectID: "pr_arrived",
	})
	if err != nil {
		t.Fatalf("TakeIn: %v", err)
	}
	if err := in.SetProject(ctx, intake, supplied.ID, "pr_elsewhere"); !errors.Is(err, intent.ErrProjectAlreadyWritten) {
		t.Errorf("SetProject on an intent that arrived with one = %v, want ErrProjectAlreadyWritten", err)
	}
}
