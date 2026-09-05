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
