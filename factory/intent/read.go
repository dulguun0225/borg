package intent

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

// Get is one intent by id. It takes the pool and not an [Intake], because
// reading an intent is not a reason to be handed the thing that writes them.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Intent, error) {
	var in Intent
	var kind, source, state string
	err := pool.QueryRow(ctx, `select id, actor_kind, actor_name, at, source, statement, state, rounds, recuts
		from `+Table+` where id = $1`, id).
		Scan(&in.ID, &kind, &in.Actor.Name, &in.At, &source, &in.Statement, &state, &in.Rounds, &in.Recuts)
	if errors.Is(err, pgx.ErrNoRows) {
		return Intent{}, fmt.Errorf("%w: %s", ErrIntentNotFound, id)
	} else if err != nil {
		return Intent{}, fmt.Errorf("intent: reading %s: %w", id, err)
	}
	in.Actor.Kind = record.Kind(kind)
	in.Source = Source(source)
	in.State = State(state)
	return in, nil
}

// Unrefined is the oldest unrefined intent whose statement is exactly this one, and
// false where none is waiting. It reads no source: an intent waiting to be refined is
// waiting whoever wrote it, and an interface that took one in on behalf of an owner
// should not take in a second copy of what a detector already asked for.
//
// It is what a caller working from a statement rather than an intent id needs, which
// is every caller that has not got a surface to pick one on. What matching on the
// statement costs is a false match where two intents say exactly the same thing, so an
// owner typing a statement character for character equal to one already waiting joins
// that intent instead of raising their own.
func Unrefined(ctx context.Context, pool *pgxpool.Pool, statement string) (Intent, bool, error) {
	if statement == "" {
		return Intent{}, false, nil
	}
	var in Intent
	var kind, source, state string
	err := pool.QueryRow(ctx, `select id, actor_kind, actor_name, at, source, statement, state, rounds, recuts
		from `+Table+` where statement = $1 and state = $2 order by at, id limit 1`,
		statement, string(StateUnrefined)).
		Scan(&in.ID, &kind, &in.Actor.Name, &in.At, &source, &in.Statement, &state, &in.Rounds, &in.Recuts)
	if errors.Is(err, pgx.ErrNoRows) {
		return Intent{}, false, nil
	} else if err != nil {
		return Intent{}, false, fmt.Errorf("intent: reading the unrefined intent for a statement: %w", err)
	}
	in.Actor.Kind = record.Kind(kind)
	in.Source = Source(source)
	in.State = State(state)
	return in, true, nil
}

// Questions is every question of one intent, in the order they were asked.
// The order is the round and not the timestamp, because the round is what the
// interview counts.
func Questions(ctx context.Context, pool *pgxpool.Pool, intentID string) ([]Question, error) {
	rows, err := pool.Query(ctx, `select id, actor_kind, actor_name, at, intent_id, round, question, answer, answered_at
		from `+QuestionTable+` where intent_id = $1 order by round`, intentID)
	if err != nil {
		return nil, fmt.Errorf("intent: reading the questions of %s: %w", intentID, err)
	}
	defer rows.Close()

	var read []Question
	for rows.Next() {
		var q Question
		var kind string
		if err := rows.Scan(&q.ID, &kind, &q.Actor.Name, &q.At, &q.IntentID,
			&q.Round, &q.Question, &q.Answer, &q.AnsweredAt); err != nil {
			return nil, fmt.Errorf("intent: reading a question of %s: %w", intentID, err)
		}
		q.Actor.Kind = record.Kind(kind)
		read = append(read, q)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("intent: reading the questions of %s: %w", intentID, err)
	}
	return read, nil
}
