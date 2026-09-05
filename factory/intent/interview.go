package intent

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// questionColumns is the question's stored fields in the order [scanQuestion]
// reads them.
const questionColumns = `id, actor_kind, actor_key, actor_key_basis, at, intent_id, round, question, answer, answered_at`

// scanQuestion reads one row of [questionColumns] into a [Question].
func scanQuestion(row pgx.Row) (Question, error) {
	var q Question
	var kind, basis string
	if err := row.Scan(&q.ID, &kind, &q.Actor.Key, &basis, &q.At, &q.IntentID,
		&q.Round, &q.Question, &q.Answer, &q.AnsweredAt); err != nil {
		return Question{}, err
	}
	q.Actor.Kind = record.Kind(kind)
	q.Actor.Basis = record.Basis(basis)
	return q, nil
}

// OpenRound advances the intent's round count by one and returns the count it
// reached. A round is opened once and however many questions are asked in it
// attach to it, because the attempt limit counts rounds: counting the
// questions instead would count a round that asked three as three.
//
// It returns the count so the caller can compare it against the attempt limit
// in force and call [Intake.Escalate] where it is exceeded. This package holds
// no limit of its own.
func (i *Intake) OpenRound(ctx context.Context, actor record.Actor, intentID string) (int, error) {
	if err := actor.Validate(); err != nil {
		return 0, err
	}
	var reached int
	err := i.write(ctx, intentID, "opening a round of", func(ctx context.Context, tx pgx.Tx, in Intent) error {
		if in.State != StateUnrefined {
			return fmt.Errorf("%w: %s is %s", ErrNotUnrefined, in.ID, in.State)
		}
		reached = in.Rounds + 1
		_, err := tx.Exec(ctx, `update `+Table+` set rounds = $1 where id = $2`, reached, in.ID)
		return err
	})
	if err != nil {
		return 0, err
	}
	return reached, nil
}

// Ask writes a question and attaches it to the open round. It advances no
// count: [Intake.OpenRound] is what advances the rounds, and an intent with no
// round open is [ErrNoOpenRound].
//
// The intent row is locked for the transaction, which is what keeps a question
// from attaching to a round that is being advanced beside it.
func (i *Intake) Ask(ctx context.Context, actor record.Actor, intentID, question string) (Question, error) {
	if err := actor.Validate(); err != nil {
		return Question{}, err
	}
	if question == "" {
		return Question{}, ErrQuestionEmpty
	}
	var asked Question
	err := i.write(ctx, intentID, "asking a question of", func(ctx context.Context, tx pgx.Tx, in Intent) error {
		if in.Rounds == 0 {
			return fmt.Errorf("%w: %s", ErrNoOpenRound, in.ID)
		}
		var err error
		asked, err = insertQuestion(ctx, tx, actor, in.ID, in.Rounds, question)
		return err
	})
	if err != nil {
		return Question{}, err
	}
	return asked, nil
}

// insertQuestion writes one question row at the round given.
func insertQuestion(ctx context.Context, tx pgx.Tx, actor record.Actor, intentID string, round int, question string) (Question, error) {
	q := Question{
		ID:       record.NewID(QuestionIDPrefix),
		Actor:    actor,
		At:       record.Now(),
		IntentID: intentID,
		Round:    round,
		Question: question,
	}
	if _, err := tx.Exec(ctx, `insert into `+QuestionTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, intent_id, round, question, answer, answered_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, '', '')`,
		q.ID, FormatVersionQuestion, string(q.Actor.Kind), q.Actor.Key, string(q.Actor.Basis),
		q.At, q.IntentID, q.Round, q.Question,
	); err != nil {
		return Question{}, fmt.Errorf("intent: writing question %s: %w", q.ID, err)
	}
	return q, nil
}

// Answer writes the answer onto the question, once. The row keeps the actor
// and the time of the ask; the answer's own time goes in answered_at, and who
// answered is validated and stored nowhere — doc.go states the cost.
//
// An empty answer is [ErrAnswerEmpty], and the answered_together constraint in
// [DDL] refuses it again: the answer is the one write-once field a human
// types, so an empty one stamps the question answered, spends the interview's
// round, and leaves the stage that asked proceeding on nothing.
func (i *Intake) Answer(ctx context.Context, actor record.Actor, questionID, answer string) (Question, error) {
	if err := actor.Validate(); err != nil {
		return Question{}, err
	}
	if answer == "" {
		return Question{}, ErrAnswerEmpty
	}

	tx, err := i.pool.Begin(ctx)
	if err != nil {
		return Question{}, fmt.Errorf("intent: beginning the answer: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, i.token); err != nil {
		return Question{}, err
	}
	answered, err := answerQuestion(ctx, tx, questionID, answer)
	if err != nil {
		return Question{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Question{}, fmt.Errorf("intent: committing the answer to %s: %w", answered.ID, err)
	}
	return answered, nil
}

// answerQuestion is the write every round's answer goes through — the
// interview's, the confirming round's, and the acceptance round's — so the
// write-once rule is implemented once whichever round the answer belongs to.
func answerQuestion(ctx context.Context, tx pgx.Tx, questionID, answer string) (Question, error) {
	if answer == "" {
		return Question{}, ErrAnswerEmpty
	}
	q, err := scanQuestion(tx.QueryRow(ctx,
		`select `+questionColumns+` from `+QuestionTable+` where id = $1 for update`, questionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Question{}, fmt.Errorf("%w: %s", ErrQuestionNotFound, questionID)
	} else if err != nil {
		return Question{}, fmt.Errorf("intent: reading question %s: %w", questionID, err)
	}
	if q.Answered() {
		return Question{}, fmt.Errorf("%w: %s", ErrAlreadyAnswered, questionID)
	}

	q.Answer = answer
	q.AnsweredAt = record.Now()
	if _, err := tx.Exec(ctx, `update `+QuestionTable+` set answer = $1, answered_at = $2 where id = $3`,
		q.Answer, q.AnsweredAt, q.ID,
	); err != nil {
		return Question{}, fmt.Errorf("intent: answering question %s: %w", q.ID, err)
	}
	return q, nil
}
