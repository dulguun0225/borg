package intent

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrSourceUnknown is returned by [Intake.TakeIn] for a source outside
	// [Sources].
	ErrSourceUnknown = errors.New("intent: the source is none of owner, reports, detector")
	// ErrStatementEmpty is returned by [Intake.TakeIn] for an intent with no
	// statement.
	ErrStatementEmpty = errors.New("intent: the statement is empty")
	// ErrQuestionEmpty is returned by [Intake.Ask] for a question with no
	// text.
	ErrQuestionEmpty = errors.New("intent: the question is empty")
	// ErrAnswerEmpty is returned by [Intake.Answer] for an answer with no
	// text. The answer is write-once and the interview is one round, so an
	// empty one would stamp the question answered with nothing in it and the
	// stage that asked would proceed on nothing.
	ErrAnswerEmpty = errors.New("intent: the answer is empty")
	// ErrIntentIDEmpty is returned by [Intake.Ask] for a question naming no
	// intent. record's doc.go states what a link is checked for.
	ErrIntentIDEmpty = errors.New("intent: the intent id is empty")
	// ErrIntentNotFound is returned where the named intent does not exist.
	ErrIntentNotFound = errors.New("intent: no intent has that id")
	// ErrQuestionNotFound is returned where the named question does not
	// exist.
	ErrQuestionNotFound = errors.New("intent: no question has that id")
	// ErrAlreadyAnswered is returned by [Intake.Answer] for a question that
	// has its answer. The answer is write-once, so an owner who answered
	// badly waits to be asked again.
	ErrAlreadyAnswered = errors.New("intent: the answer is write-once, and this question has one")
	// ErrNotUnrefined is returned by [Intake.MarkRefined] for an intent whose
	// state is not unrefined. Refined does not advance further here, and
	// nothing un-refines.
	ErrNotUnrefined = errors.New("intent: only an unrefined intent is marked refined")
)

// Intake is the one writer of intents and their questions. The three sources
// are three callers of this one entrance, which is why every method takes the
// actor rather than the writer holding one.
type Intake struct {
	pool *pgxpool.Pool
}

// NewIntake returns the writer over pool.
func NewIntake(pool *pgxpool.Pool) *Intake { return &Intake{pool: pool} }

// TakeIn writes an intent as it arrives: unrefined, zero rounds, and judged
// by nothing on the way in, because judging it is what the interview is for.
func (i *Intake) TakeIn(ctx context.Context, actor record.Actor, source Source, statement string) (Intent, error) {
	if err := actor.Validate(); err != nil {
		return Intent{}, err
	}
	if !slices.Contains(Sources, source) {
		return Intent{}, fmt.Errorf("%w: %q", ErrSourceUnknown, source)
	}
	if statement == "" {
		return Intent{}, ErrStatementEmpty
	}

	in := Intent{
		ID:        record.NewID(IDPrefix),
		Actor:     actor,
		At:        record.Now(),
		Source:    source,
		Statement: statement,
		State:     StateUnrefined,
		Rounds:    0,
	}
	_, err := i.pool.Exec(ctx, `insert into `+Table+`
		(id, actor_kind, actor_name, at, source, statement, state, rounds)
		values ($1, $2, $3, $4, $5, $6, $7, $8)`,
		in.ID, string(in.Actor.Kind), in.Actor.Name, in.At,
		string(in.Source), in.Statement, string(in.State), in.Rounds,
	)
	if err != nil {
		return Intent{}, fmt.Errorf("intent: taking in %s: %w", in.ID, err)
	}
	return in, nil
}

// Ask writes a question and starts a new round: the question's round is the
// intent's round count plus one, and the count advances in the same
// transaction, so the two cannot disagree. The intent row is locked for the
// transaction, which is what keeps two concurrent questions out of one round.
func (i *Intake) Ask(ctx context.Context, actor record.Actor, intentID, question string) (Question, error) {
	if err := actor.Validate(); err != nil {
		return Question{}, err
	}
	if intentID == "" {
		return Question{}, ErrIntentIDEmpty
	}
	if question == "" {
		return Question{}, ErrQuestionEmpty
	}

	tx, err := i.pool.Begin(ctx)
	if err != nil {
		return Question{}, fmt.Errorf("intent: beginning the ask: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var rounds int
	err = tx.QueryRow(ctx, `select rounds from `+Table+` where id = $1 for update`, intentID).Scan(&rounds)
	if errors.Is(err, pgx.ErrNoRows) {
		return Question{}, fmt.Errorf("%w: %s", ErrIntentNotFound, intentID)
	} else if err != nil {
		return Question{}, fmt.Errorf("intent: reading %s: %w", intentID, err)
	}

	q := Question{
		ID:       record.NewID(QuestionIDPrefix),
		Actor:    actor,
		At:       record.Now(),
		IntentID: intentID,
		Round:    rounds + 1,
		Question: question,
	}
	if _, err := tx.Exec(ctx, `insert into `+QuestionTable+`
		(id, actor_kind, actor_name, at, intent_id, round, question, answer, answered_at)
		values ($1, $2, $3, $4, $5, $6, $7, '', '')`,
		q.ID, string(q.Actor.Kind), q.Actor.Name, q.At, q.IntentID, q.Round, q.Question,
	); err != nil {
		return Question{}, fmt.Errorf("intent: writing question %s: %w", q.ID, err)
	}
	if _, err := tx.Exec(ctx, `update `+Table+` set rounds = $1 where id = $2`, q.Round, intentID); err != nil {
		return Question{}, fmt.Errorf("intent: advancing the rounds of %s: %w", intentID, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Question{}, fmt.Errorf("intent: committing question %s: %w", q.ID, err)
	}
	return q, nil
}

// Answer writes the answer onto the question, once. The row keeps the actor
// and the time of the ask; the answer's own time goes in answered_at, and who
// answered is validated and stored nowhere — doc.go states the cost.
//
// An empty answer is [ErrAnswerEmpty], and the answered_together constraint in
// [DDL] refuses it again: the answer is the one write-once field a human types,
// so an empty one stamps the question answered, spends the interview's round,
// and leaves the stage that asked proceeding on nothing.
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

	var q Question
	var kind string
	err = tx.QueryRow(ctx, `select id, actor_kind, actor_name, at, intent_id, round, question, answer, answered_at
		from `+QuestionTable+` where id = $1 for update`, questionID).
		Scan(&q.ID, &kind, &q.Actor.Name, &q.At, &q.IntentID, &q.Round, &q.Question, &q.Answer, &q.AnsweredAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Question{}, fmt.Errorf("%w: %s", ErrQuestionNotFound, questionID)
	} else if err != nil {
		return Question{}, fmt.Errorf("intent: reading question %s: %w", questionID, err)
	}
	q.Actor.Kind = record.Kind(kind)

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

	if err := tx.Commit(ctx); err != nil {
		return Question{}, fmt.Errorf("intent: committing the answer to %s: %w", q.ID, err)
	}
	return q, nil
}

// MarkRefined advances the intent from unrefined to refined, the one
// transition this package writes. Any other starting state is refused: refined
// advances no further here, and nothing un-refines.
func (i *Intake) MarkRefined(ctx context.Context, actor record.Actor, intentID string) error {
	if err := actor.Validate(); err != nil {
		return err
	}

	tx, err := i.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("intent: beginning the refinement of %s: %w", intentID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var state string
	err = tx.QueryRow(ctx, `select state from `+Table+` where id = $1 for update`, intentID).Scan(&state)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrIntentNotFound, intentID)
	} else if err != nil {
		return fmt.Errorf("intent: reading %s: %w", intentID, err)
	}
	if State(state) != StateUnrefined {
		return fmt.Errorf("%w: %s is %s", ErrNotUnrefined, intentID, state)
	}

	if _, err := tx.Exec(ctx, `update `+Table+` set state = $1 where id = $2`, string(StateRefined), intentID); err != nil {
		return fmt.Errorf("intent: marking %s refined: %w", intentID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("intent: committing the refinement of %s: %w", intentID, err)
	}
	return nil
}
