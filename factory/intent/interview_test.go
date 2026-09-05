package intent_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/record"
)

// confirmEnumerated refines an intent the factory raised: it takes no
// confirming round, and enumerates its requirements from the evidence instead.
func confirmEnumerated(t *testing.T, ctx context.Context, in *intent.Intake, intentID string) []intent.Requirement {
	t.Helper()
	written, err := in.Confirm(ctx, intake, intent.Confirmation{
		IntentID: intentID,
		Requirements: []intent.NewRequirement{
			{Statement: "When the release is reverted, the system shall serve the release below it."},
		},
	})
	if err != nil {
		t.Fatalf("Confirm on an intent the factory raised: %v", err)
	}
	return written
}

// confirmed runs a requested intent through one round and its confirming
// round, and returns the intent's id.
func confirmed(t *testing.T, ctx context.Context, in *intent.Intake, statement string,
	requirements ...intent.NewRequirement,
) string {
	t.Helper()
	taken := requested(t, ctx, in, statement)
	if _, err := in.OpenRound(ctx, intake, taken.ID); err != nil {
		t.Fatalf("OpenRound: %v", err)
	}
	asked, err := in.Ask(ctx, intake, taken.ID, "Is this what you asked for?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if _, err := in.Confirm(ctx, intake, intent.Confirmation{
		IntentID:       taken.ID,
		QuestionID:     asked.ID,
		Answer:         "Yes.",
		IntendedEffect: "A shopper whose card fails once still completes the order.",
		Tier:           intent.Tier{Value: 2, PolicyVersion: "pv_1"},
		Requirements:   requirements,
	}); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	return taken.ID
}

// TestARoundIsOpenedOnceAndQuestionsAttachToIt: the attempt limit counts
// rounds, so a round that asks three questions is one round and not three.
func TestARoundIsOpenedOnceAndQuestionsAttachToIt(t *testing.T) {
	ctx, pool, in := newIntake(t)
	taken := requested(t, ctx, in, "checkout should retry")

	if _, err := in.Ask(ctx, intake, taken.ID, "Against which provider?"); !errors.Is(err, intent.ErrNoOpenRound) {
		t.Errorf("Ask before a round was opened = %v, want ErrNoOpenRound", err)
	}

	round, err := in.OpenRound(ctx, intake, taken.ID)
	if err != nil {
		t.Fatalf("OpenRound: %v", err)
	}
	if round != 1 {
		t.Errorf("the first round is %d, want 1", round)
	}

	first, err := in.Ask(ctx, intake, taken.ID, "Against which provider?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	second, err := in.Ask(ctx, intake, taken.ID, "Once per charge or once per session?")
	if err != nil {
		t.Fatalf("Ask again: %v", err)
	}
	third, err := in.Ask(ctx, intake, taken.ID, "How long between the two attempts?")
	if err != nil {
		t.Fatalf("Ask a third time: %v", err)
	}
	for _, q := range []intent.Question{first, second, third} {
		if q.Round != 1 {
			t.Errorf("question %s is at round %d, want the one round that is open", q.ID, q.Round)
		}
		if q.Answered() || q.Answer != "" || q.AnsweredAt != "" {
			t.Errorf("a new question reads as answered: %+v", q)
		}
	}

	read, err := intent.Get(ctx, pool, taken.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Rounds != 1 {
		t.Errorf("three questions in one round count %d rounds, want 1", read.Rounds)
	}

	if _, err := in.OpenRound(ctx, intake, taken.ID); err != nil {
		t.Fatalf("OpenRound again: %v", err)
	}
	fourth, err := in.Ask(ctx, intake, taken.ID, "And on the second failure?")
	if err != nil {
		t.Fatalf("Ask in the second round: %v", err)
	}
	if fourth.Round != 2 {
		t.Errorf("the fourth question is at round %d, want 2", fourth.Round)
	}

	questions, err := intent.Questions(ctx, pool, taken.ID)
	if err != nil {
		t.Fatalf("Questions: %v", err)
	}
	if len(questions) != 4 || questions[0].ID != first.ID || questions[3].ID != fourth.ID {
		t.Errorf("Questions = %+v, want the four asked in round order", questions)
	}

	if _, err := in.OpenRound(ctx, intake, "in_missing"); !errors.Is(err, intent.ErrIntentNotFound) {
		t.Errorf("OpenRound on a missing intent = %v, want ErrIntentNotFound", err)
	}
	if _, err := in.Ask(ctx, intake, taken.ID, ""); !errors.Is(err, intent.ErrQuestionEmpty) {
		t.Errorf("Ask with no question = %v, want ErrQuestionEmpty", err)
	}
	// An empty link names nothing, and the writer refuses it the way it
	// refuses every other required field. record's doc.go states what a link
	// is checked for.
	if _, err := in.Ask(ctx, intake, "", "anything?"); !errors.Is(err, intent.ErrIntentIDEmpty) {
		t.Errorf("Ask naming no intent = %v, want ErrIntentIDEmpty", err)
	}
}

func TestAnswerIsWriteOnce(t *testing.T) {
	ctx, pool, in := newIntake(t)
	taken := requested(t, ctx, in, "checkout should retry")
	if _, err := in.OpenRound(ctx, intake, taken.ID); err != nil {
		t.Fatalf("OpenRound: %v", err)
	}
	asked, err := in.Ask(ctx, intake, taken.ID, "Retry against which payment provider?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	answered, err := in.Answer(ctx, owner, asked.ID, "The primary one only.")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if answered.Answer != "The primary one only." || !answered.Answered() {
		t.Errorf("the answer was not written: %+v", answered)
	}
	if _, err := time.Parse(record.TimeLayout, answered.AnsweredAt); err != nil {
		t.Errorf("answered_at %q: %v", answered.AnsweredAt, err)
	}
	// The row keeps the actor and the time of the ask.
	if answered.Actor != asked.Actor || answered.At != asked.At {
		t.Errorf("the answer rewrote the ask's actor or time: %+v", answered)
	}

	questions, err := intent.Questions(ctx, pool, taken.ID)
	if err != nil {
		t.Fatalf("Questions: %v", err)
	}
	if len(questions) != 1 || questions[0].ID != answered.ID || !questions[0].Answered() {
		t.Errorf("Questions = %+v, want the answered question, %+v", questions, answered)
	}

	if _, err := in.Answer(ctx, owner, asked.ID, "No, both."); !errors.Is(err, intent.ErrAlreadyAnswered) {
		t.Errorf("Answer on an answered question = %v, want ErrAlreadyAnswered", err)
	}
	if _, err := in.Answer(ctx, owner, "q_missing", "anything"); !errors.Is(err, intent.ErrQuestionNotFound) {
		t.Errorf("Answer on a missing question = %v, want ErrQuestionNotFound", err)
	}
}

// TestAnEmptyAnswerIsRefused is the one write-once field a human types. An
// empty answer would stamp the question answered with nothing in it, and the
// retry after it is ErrAlreadyAnswered forever, so it is refused before it is
// written.
func TestAnEmptyAnswerIsRefused(t *testing.T) {
	ctx, pool, in := newIntake(t)
	taken := requested(t, ctx, in, "checkout should retry")
	if _, err := in.OpenRound(ctx, intake, taken.ID); err != nil {
		t.Fatalf("OpenRound: %v", err)
	}
	asked, err := in.Ask(ctx, intake, taken.ID, "Retry against which payment provider?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if _, err := in.Answer(ctx, owner, asked.ID, ""); !errors.Is(err, intent.ErrAnswerEmpty) {
		t.Errorf("Answer with no answer = %v, want ErrAnswerEmpty", err)
	}
	read, err := intent.Questions(ctx, pool, taken.ID)
	if err != nil {
		t.Fatalf("Questions: %v", err)
	}
	if len(read) != 1 || read[0].Answered() {
		t.Errorf("Questions = %+v, want the one question still unanswered", read)
	}
	if _, err := in.Answer(ctx, owner, asked.ID, "The primary one only."); err != nil {
		t.Errorf("Answer after the refusal: %v, want the question still answerable", err)
	}
}

// TestTheConfirmingRoundWritesTheReadingTheEffectAndTheTier: the round that
// ends the interview writes all three in one transaction and moves the intent
// to refined.
func TestTheConfirmingRoundWritesTheReadingTheEffectAndTheTier(t *testing.T) {
	ctx, pool, in := newIntake(t)
	taken := requested(t, ctx, in, "checkout should retry a failed charge once")
	if _, err := in.OpenRound(ctx, intake, taken.ID); err != nil {
		t.Fatalf("OpenRound: %v", err)
	}
	asked, err := in.Ask(ctx, intake, taken.ID,
		"You want one retry per charge against the primary provider. Is that right?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	written, err := in.Confirm(ctx, intake, intent.Confirmation{
		IntentID:       taken.ID,
		QuestionID:     asked.ID,
		Answer:         "Yes, that is what I meant.",
		IntendedEffect: "A shopper whose card fails once still completes the order.",
		Tier:           intent.Tier{Value: 2, PolicyVersion: "pv_1"},
		Requirements: []intent.NewRequirement{
			{Statement: "When a charge fails, the system shall retry it once against the primary provider."},
			{Statement: "If the retry fails, then the system shall show the shopper the failure."},
		},
	})
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if len(written) != 2 {
		t.Fatalf("Confirm wrote %d requirements, want 2", len(written))
	}
	if written[0].Pattern != intent.PatternEvent || written[1].Pattern != intent.PatternUnwantedCondition {
		t.Errorf("the reading was classified %s and %s, want event and unwanted_condition",
			written[0].Pattern, written[1].Pattern)
	}
	for _, r := range written {
		if r.Kind != intent.KindConfirmed || !r.InForce() || r.IntentID != taken.ID {
			t.Errorf("requirement %+v is not a confirmed requirement in force of this intent", r)
		}
	}

	read, err := intent.Get(ctx, pool, taken.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.State != intent.StateRefined {
		t.Errorf("the intent is %s, want refined", read.State)
	}
	if read.IntendedEffect == "" || read.Tier != (intent.Tier{Value: 2, PolicyVersion: "pv_1"}) {
		t.Errorf("the confirming round left the effect %q and the tier %+v", read.IntendedEffect, read.Tier)
	}
	if read.Rounds != 1 {
		t.Errorf("the confirming round left %d rounds, want the one it answered", read.Rounds)
	}

	answers, err := intent.Questions(ctx, pool, taken.ID)
	if err != nil {
		t.Fatalf("Questions: %v", err)
	}
	if len(answers) != 1 || !answers[0].Answered() {
		t.Errorf("Questions = %+v, want the confirming question answered", answers)
	}

	// Refined does not confirm again.
	if _, err := in.Confirm(ctx, intake, intent.Confirmation{
		IntentID: taken.ID, QuestionID: asked.ID, Answer: "again",
		IntendedEffect: "again", Tier: intent.Tier{Value: 1, PolicyVersion: "pv_1"},
		Requirements: []intent.NewRequirement{{Statement: "The system shall do it again."}},
	}); !errors.Is(err, intent.ErrNotUnrefined) {
		t.Errorf("Confirm on a refined intent = %v, want ErrNotUnrefined", err)
	}
}

// TestTheConfirmingRoundIsOwedByARequesterAndNotByTheFactory is the design's
// asymmetry: an intent somebody asked for owes a question, an answer, an
// intended effect and a tier, and one the factory raised is refused all four.
func TestTheConfirmingRoundIsOwedByARequesterAndNotByTheFactory(t *testing.T) {
	ctx, pool, in := newIntake(t)
	taken := requested(t, ctx, in, "checkout should retry")
	if _, err := in.OpenRound(ctx, intake, taken.ID); err != nil {
		t.Fatalf("OpenRound: %v", err)
	}
	asked, err := in.Ask(ctx, intake, taken.ID, "Is this right?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	statement := []intent.NewRequirement{
		{Statement: "When a charge fails, the system shall retry it once."},
	}

	for _, refused := range []struct {
		name         string
		confirmation intent.Confirmation
		want         error
	}{
		{"no requirements at all",
			intent.Confirmation{IntentID: taken.ID}, intent.ErrRequirementsEmpty},
		{"no confirming question",
			intent.Confirmation{IntentID: taken.ID, Requirements: statement}, intent.ErrRequesterOwed},
		{"no intended effect",
			intent.Confirmation{IntentID: taken.ID, QuestionID: asked.ID, Answer: "Yes.",
				Tier: intent.Tier{Value: 1, PolicyVersion: "pv_1"}, Requirements: statement},
			intent.ErrIntendedEffectEmpty},
		{"no tier",
			intent.Confirmation{IntentID: taken.ID, QuestionID: asked.ID, Answer: "Yes.",
				IntendedEffect: "who it is for", Requirements: statement}, intent.ErrRequesterOwed},
		{"a statement fitting no pattern and carrying no reason",
			intent.Confirmation{IntentID: taken.ID, QuestionID: asked.ID, Answer: "Yes.",
				IntendedEffect: "who it is for", Tier: intent.Tier{Value: 1, PolicyVersion: "pv_1"},
				Requirements: []intent.NewRequirement{{Statement: "Make checkout better."}}},
			intent.ErrEscapeReasonMissing},
		{"a reason on a statement that fits one",
			intent.Confirmation{IntentID: taken.ID, QuestionID: asked.ID, Answer: "Yes.",
				IntendedEffect: "who it is for", Tier: intent.Tier{Value: 1, PolicyVersion: "pv_1"},
				Requirements: []intent.NewRequirement{{
					Statement:    "When a charge fails, the system shall retry it once.",
					EscapeReason: "not a sentence anybody could pattern",
				}}},
			intent.ErrEscapeReasonUnwanted},
	} {
		if _, err := in.Confirm(ctx, intake, refused.confirmation); !errors.Is(err, refused.want) {
			t.Errorf("Confirm with %s = %v, want %v", refused.name, err, refused.want)
		}
	}

	// The factory's own is refused each of the four rather than owing them.
	own := raised(t, ctx, in, crossing, "Revert release 9 of checkout.")
	for _, refused := range []struct {
		name         string
		confirmation intent.Confirmation
	}{
		{"a confirming question", intent.Confirmation{IntentID: own.ID, QuestionID: asked.ID, Requirements: statement}},
		{"an intended effect", intent.Confirmation{IntentID: own.ID, IntendedEffect: "who", Requirements: statement}},
		{"a proposed tier", intent.Confirmation{IntentID: own.ID,
			Tier: intent.Tier{Value: 1, PolicyVersion: "pv_1"}, Requirements: statement}},
	} {
		if _, err := in.Confirm(ctx, intake, refused.confirmation); !errors.Is(err, intent.ErrNoRequester) {
			t.Errorf("Confirm on the factory's own with %s = %v, want ErrNoRequester", refused.name, err)
		}
	}

	enumerated := confirmEnumerated(t, ctx, in, own.ID)
	if len(enumerated) != 1 || enumerated[0].Kind != intent.KindEnumeratedFromEvidence {
		t.Errorf("the factory's own enumerated %+v, want one requirement enumerated from evidence", enumerated)
	}
	read, err := intent.Get(ctx, pool, own.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.State != intent.StateRefined || read.Rounds != 0 {
		t.Errorf("the factory's own is %s at %d rounds, want refined at 0 — it takes no round",
			read.State, read.Rounds)
	}
}

// TestACorrectionAttachesAndAsksAgain: corrected, the answer attaches like any
// other, the factory asks again, the correction counts a round, and no
// requirement is written — the round that confirms a reading is what writes
// one.
func TestACorrectionAttachesAndAsksAgain(t *testing.T) {
	ctx, pool, in := newIntake(t)
	taken := requested(t, ctx, in, "checkout should retry")
	if _, err := in.OpenRound(ctx, intake, taken.ID); err != nil {
		t.Fatalf("OpenRound: %v", err)
	}
	asked, err := in.Ask(ctx, intake, taken.ID, "One retry per charge. Is that right?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	again, err := in.Correct(ctx, intake, intent.Correction{
		IntentID:   taken.ID,
		QuestionID: asked.ID,
		Correction: "No — one retry per session.",
		Question:   "One retry per session. Is that right?",
	})
	if err != nil {
		t.Fatalf("Correct: %v", err)
	}
	if again.Round != 2 {
		t.Errorf("the correction asked again at round %d, want 2", again.Round)
	}

	read, err := intent.Get(ctx, pool, taken.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.State != intent.StateUnrefined || read.Rounds != 2 {
		t.Errorf("after a correction the intent is %s at %d rounds, want unrefined at 2", read.State, read.Rounds)
	}
	requirements, err := intent.Requirements(ctx, pool, taken.ID)
	if err != nil {
		t.Fatalf("Requirements: %v", err)
	}
	if len(requirements) != 0 {
		t.Errorf("a corrected round wrote %d requirements, want none", len(requirements))
	}
	questions, err := intent.Questions(ctx, pool, taken.ID)
	if err != nil {
		t.Fatalf("Questions: %v", err)
	}
	if len(questions) != 2 || !questions[0].Answered() || questions[0].Answer != "No — one retry per session." {
		t.Errorf("Questions = %+v, want the correction attached to the first and the second asked", questions)
	}

	own := raised(t, ctx, in, crossing, "Revert release 9 of checkout.")
	if _, err := in.Correct(ctx, intake, intent.Correction{
		IntentID: own.ID, QuestionID: asked.ID, Correction: "no", Question: "again?",
	}); !errors.Is(err, intent.ErrNoRequester) {
		t.Errorf("Correct on the factory's own = %v, want ErrNoRequester", err)
	}
}
