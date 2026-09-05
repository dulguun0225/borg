package intent

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Get is one intent by id. It takes the pool and not an [Intake], because
// reading an intent is not a reason to be handed the thing that writes them.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Intent, error) {
	in, err := scanIntent(pool.QueryRow(ctx, `select `+intentColumns+` from `+Table+` where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Intent{}, fmt.Errorf("%w: %s", ErrIntentNotFound, id)
	} else if err != nil {
		return Intent{}, fmt.Errorf("intent: reading %s: %w", id, err)
	}
	return in, nil
}

// OnEvidence is the oldest intent on this evidence that has not finished, and
// false where there is none. It is what a detector runs before it raises one:
// the factory's own source repeats where the other two do not — a health
// monitor still running is still running tomorrow — so a detector raises an
// intent for a condition and not for an observation, and attaches to the one
// already open instead.
//
// Not finished is any state but delivered and dropped, so a condition that
// returns after the intent that answered it closed raises a new intent rather
// than reopening a closed one.
//
// The evidence is what keys it: this service, this consumer, this contract and
// element, this release, this constraint, this objective's period. Keyed too
// finely it raises an intent per observation; too coarsely it attaches two
// problems to one intent, which decomposition then has to split. An empty
// evidence keys nothing and matches nothing.
func OnEvidence(ctx context.Context, pool *pgxpool.Pool, evidence Evidence) (Intent, bool, error) {
	key, err := evidence.Key()
	if err != nil {
		return Intent{}, false, err
	}
	if key == "" {
		return Intent{}, false, nil
	}
	in, err := scanIntent(pool.QueryRow(ctx, `select `+intentColumns+` from `+Table+`
		where evidence = $1 and state not in ($2, $3) order by at, id limit 1`,
		key, string(StateDelivered), string(StateDropped)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Intent{}, false, nil
	} else if err != nil {
		return Intent{}, false, fmt.Errorf("intent: reading the intent on an evidence: %w", err)
	}
	return in, true, nil
}

// Questions is every question of one intent, in the order they were asked.
// The order is the round and not the timestamp, because the round is what the
// interview counts.
func Questions(ctx context.Context, pool *pgxpool.Pool, intentID string) ([]Question, error) {
	rows, err := pool.Query(ctx, `select `+questionColumns+`
		from `+QuestionTable+` where intent_id = $1 order by round, at, id`, intentID)
	if err != nil {
		return nil, fmt.Errorf("intent: reading the questions of %s: %w", intentID, err)
	}
	defer rows.Close()

	var read []Question
	for rows.Next() {
		q, err := scanQuestion(rows)
		if err != nil {
			return nil, fmt.Errorf("intent: reading a question of %s: %w", intentID, err)
		}
		read = append(read, q)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("intent: reading the questions of %s: %w", intentID, err)
	}
	return read, nil
}

// Requirements is the intent's reading in force: the set the last confirming
// round wrote and no earlier one, with the shares decomposition derived from
// it. Decomposition's completeness check reads this and nothing superseded, so
// a re-decomposition is never asked to answer a statement the requester
// withdrew.
func Requirements(ctx context.Context, pool *pgxpool.Pool, intentID string) ([]Requirement, error) {
	rows, err := pool.Query(ctx, `select `+requirementColumns+` from `+RequirementTable+`
		where intent_id = $1 and superseded_at = '' order by at, id`, intentID)
	if err != nil {
		return nil, fmt.Errorf("intent: reading the requirements of %s: %w", intentID, err)
	}
	defer rows.Close()
	return collectRequirements(rows, intentID)
}

// EveryRequirement is the intent's requirements whether or not they are in
// force, which is what reads through a supersession pointer to what replaced a
// statement — the reading a criterion in force for a merged item names.
func EveryRequirement(ctx context.Context, pool *pgxpool.Pool, intentID string) ([]Requirement, error) {
	rows, err := pool.Query(ctx, `select `+requirementColumns+` from `+RequirementTable+`
		where intent_id = $1 order by at, id`, intentID)
	if err != nil {
		return nil, fmt.Errorf("intent: reading the requirements of %s: %w", intentID, err)
	}
	defer rows.Close()
	return collectRequirements(rows, intentID)
}

// ForItem is the requirements derived for one item's share, in force. What an
// item answers whole is a field of the item and not a field here, so this
// returns the shares and nothing else.
func ForItem(ctx context.Context, pool *pgxpool.Pool, itemID string) ([]Requirement, error) {
	rows, err := pool.Query(ctx, `select `+requirementColumns+` from `+RequirementTable+`
		where item_id = $1 and superseded_at = '' order by at, id`, itemID)
	if err != nil {
		return nil, fmt.Errorf("intent: reading the requirements of item %s: %w", itemID, err)
	}
	defer rows.Close()
	return collectRequirements(rows, itemID)
}

// Escaped is how many of an intent's requirements in force fit none of the six
// patterns, beside how many there are: the count a statement admitted with a
// tagged reason is counted in, which Factory reports as a share beside the
// share of criteria in the unwanted-condition pattern. Nothing decides on it.
func Escaped(ctx context.Context, pool *pgxpool.Pool, intentID string) (escaped, total int, err error) {
	err = pool.QueryRow(ctx, `select count(*) filter (where pattern = ''), count(*)
		from `+RequirementTable+` where intent_id = $1 and superseded_at = ''`, intentID).
		Scan(&escaped, &total)
	if err != nil {
		return 0, 0, fmt.Errorf("intent: counting the escaped requirements of %s: %w", intentID, err)
	}
	return escaped, total, nil
}
