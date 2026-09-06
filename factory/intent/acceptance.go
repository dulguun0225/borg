package intent

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/record"
)

// AcceptanceRound asks the one round that follows production: what was asked
// for, the intended effect the requester confirmed, what shipped, and the
// releases that carry it, and what it asks is whether the effect was had. It
// is a question record like any other, written here and answered at Work.
//
// Whether every item of the intent is live is the caller's read, over the
// items and their production deploy records, and this package writes no item.
// What is refused here is what the intent itself says: an intent the factory
// raised takes no such round, nobody being able to say the evidence was
// misread, and one not refined has not shipped.
//
// It opens a round of its own and the round count advances, because the count
// is what the attempt limit reads and this is a round. A correction to it then
// costs that one round and no second.
//
// The round is what waits on the requester, so the notifier is told once it is
// written, the way [Intake.Ask] tells it about a round of the interview: the
// write is committed first, and a delivery that failed is returned with the
// round already asked.
func (i *Intake) AcceptanceRound(ctx context.Context, actor record.Actor, intentID, question string) (Question, error) {
	if err := actor.Validate(); err != nil {
		return Question{}, err
	}
	if question == "" {
		return Question{}, ErrQuestionEmpty
	}
	if i.notifier == nil {
		return Question{}, fmt.Errorf("%w: asking the acceptance round of %s", ErrNotifierNotComposed, intentID)
	}
	var asked Question
	err := i.write(ctx, intentID, "asking the acceptance round of", func(ctx context.Context, tx pgx.Tx, in Intent) error {
		if in.Source == SourceDetector {
			return fmt.Errorf("%w: %s takes no acceptance round", ErrNoRequester, in.ID)
		}
		if in.State != StateRefined {
			return fmt.Errorf("%w: %s is %s", ErrNotRefined, in.ID, in.State)
		}
		round := in.Rounds + 1
		if _, err := tx.Exec(ctx, `update `+Table+` set rounds = $1 where id = $2`, round, in.ID); err != nil {
			return err
		}
		var err error
		asked, err = insertQuestion(ctx, tx, actor, in.ID, round, question)
		return err
	})
	if err != nil {
		return Question{}, err
	}
	if err := i.notifier.AcceptanceRound(ctx, asked.IntentID, asked.ID, asked.Question); err != nil {
		return asked, fmt.Errorf("intent: telling a human about the acceptance round %s: %w", asked.ID, err)
	}
	return asked, nil
}

// Delivery is the close of an intent: the acceptance round's answer and the
// outcome computed from it. An intent the factory raised is delivered when its
// last item goes live and carries none of the three, so QuestionID, Answer and
// Outcome are all refused on one.
type Delivery struct {
	IntentID string
	// QuestionID is the acceptance round's question, answered with Answer in
	// this call.
	QuestionID string
	Answer     string
	// Outcome is the intent's outcome, computed once at this close: the
	// acceptance round's verdict on the intended effect for a requested
	// intent, and the rate of reports before and after the release for one
	// grouped from reports. The rate's input is the report store, which is not
	// built, so the caller supplies the value either way.
	Outcome string
}

// Delivered writes delivered where the person who asked confirmed that what
// shipped is what they asked for, with the outcome the close computed. It is
// the verdict on the intended effect and not on the reading.
func (i *Intake) Delivered(ctx context.Context, actor record.Actor, delivery Delivery) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	return i.write(ctx, delivery.IntentID, "delivering", func(ctx context.Context, tx pgx.Tx, in Intent) error {
		if in.State != StateRefined {
			return fmt.Errorf("%w: %s is %s", ErrNotRefined, in.ID, in.State)
		}
		if in.Source == SourceDetector {
			for _, unwanted := range []struct{ what, value string }{
				{"an acceptance round", delivery.QuestionID},
				{"an answer", delivery.Answer},
				{"an outcome", delivery.Outcome},
			} {
				if unwanted.value != "" {
					return fmt.Errorf("%w: %s takes no %s", ErrNoRequester, in.ID, unwanted.what)
				}
			}
		} else {
			if delivery.QuestionID == "" {
				return fmt.Errorf("%w: %s owes an acceptance round", ErrRequesterOwed, in.ID)
			}
			if delivery.Outcome == "" {
				return fmt.Errorf("%w: %s", ErrOutcomeEmpty, in.ID)
			}
			answered, err := answerQuestion(ctx, tx, delivery.QuestionID, delivery.Answer)
			if err != nil {
				return err
			}
			if answered.IntentID != in.ID {
				return fmt.Errorf("%w: %s is of %s", ErrQuestionElsewhere, answered.ID, answered.IntentID)
			}
		}
		_, err := tx.Exec(ctx, `update `+Table+` set state = $1, outcome = $2 where id = $3`,
			string(StateDelivered), delivery.Outcome, in.ID)
		return err
	})
}

// CorrectAcceptance records a correction at the acceptance round: the
// correction attaches as the answer to the question that was asked, and the
// intent goes back to unrefined, which reopens the interview the way a
// replacement constraint's raise does. The round it costs was counted when
// [Intake.AcceptanceRound] asked it.
func (i *Intake) CorrectAcceptance(ctx context.Context, actor record.Actor, intentID, questionID, correction string) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if correction == "" {
		return ErrAnswerEmpty
	}
	return i.write(ctx, intentID, "correcting the acceptance round of", func(ctx context.Context, tx pgx.Tx, in Intent) error {
		if in.State != StateRefined {
			return fmt.Errorf("%w: %s is %s", ErrNotRefined, in.ID, in.State)
		}
		if in.Source == SourceDetector {
			return fmt.Errorf("%w: %s takes no acceptance round", ErrNoRequester, in.ID)
		}
		answered, err := answerQuestion(ctx, tx, questionID, correction)
		if err != nil {
			return err
		}
		if answered.IntentID != in.ID {
			return fmt.Errorf("%w: %s is of %s", ErrQuestionElsewhere, answered.ID, answered.IntentID)
		}
		return sendBack(ctx, tx, in.ID, SentBackByAcceptanceCorrection)
	})
}
