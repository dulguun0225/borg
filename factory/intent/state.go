package intent

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/record"
)

// SendBack writes unrefined again, naming what sent it back, which is what
// reopens the interview. There are four: a rework request naming the intent, a
// gate's reject naming it, a replacement constraint's raise landing on it, and
// a correction at the acceptance round — the last written by
// [Intake.CorrectAcceptance] rather than here.
//
// Three states refuse it. An intent already dropped or delivered is
// [ErrFinished]: both are ends. An escalated one is [ErrEscalated], an
// escalation staying a human's to clear. A re-decomposing one is
// [ErrReDecomposing], an open Decomposition firing closing first and the
// send-back landing then, which is a second call by the caller that made this
// one — no field here remembers it.
func (i *Intake) SendBack(ctx context.Context, actor record.Actor, intentID string, by SentBackBy) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if !slices.Contains(SentBackBys, by) {
		return fmt.Errorf("%w: %q", ErrSentBackByUnknown, by)
	}
	return i.write(ctx, intentID, "sending back", func(ctx context.Context, tx pgx.Tx, in Intent) error {
		if finished(in.State) {
			return fmt.Errorf("%w: %s is %s", ErrFinished, in.ID, in.State)
		}
		switch in.State {
		case StateEscalated:
			return fmt.Errorf("%w: %s", ErrEscalated, in.ID)
		case StateReDecomposing:
			return fmt.Errorf("%w: %s", ErrReDecomposing, in.ID)
		}
		return sendBack(ctx, tx, in.ID, by)
	})
}

// sendBack is the update every send-back makes, so the state and the cause are
// written together whichever call reached it.
func sendBack(ctx context.Context, tx pgx.Tx, intentID string, by SentBackBy) error {
	_, err := tx.Exec(ctx, `update `+Table+` set state = $1, sent_back_by = $2 where id = $3`,
		string(StateUnrefined), string(by), intentID)
	if err != nil {
		return fmt.Errorf("intent: sending %s back: %w", intentID, err)
	}
	return nil
}

// MarkReDecomposing writes re-decomposing at a re-decomposition's open and
// advances the re-decomposition count by one, returning the count it reached.
// The state is what stops every unmerged item of that intent while the
// Decomposition firing is open — dispatch, the gate component, and the merge
// queue each read it — and without it a sibling merges between the rework
// request and the verdict.
//
// The count is a field of its own beside the rounds and never the same field.
// The two are different stretches of work: an owner answering an escalated
// interview clears that count alone, and one field would spend an interview's
// rounds out of decomposition's budget. The count is returned so the caller can
// compare it against the attempt limit in force and call [Intake.Escalate]
// where it is exceeded.
func (i *Intake) MarkReDecomposing(ctx context.Context, actor record.Actor, intentID string) (int, error) {
	if err := actor.Validate(); err != nil {
		return 0, err
	}
	var reached int
	err := i.write(ctx, intentID, "opening a re-decomposition of", func(ctx context.Context, tx pgx.Tx, in Intent) error {
		if in.State != StateRefined {
			return fmt.Errorf("%w: %s is %s", ErrNotRefined, in.ID, in.State)
		}
		reached = in.ReDecompositions + 1
		_, err := tx.Exec(ctx, `update `+Table+` set state = $1, re_decompositions = $2 where id = $3`,
			string(StateReDecomposing), reached, in.ID)
		return err
	})
	if err != nil {
		return 0, err
	}
	return reached, nil
}

// ClearReDecomposing writes refined again at the re-decomposition's close,
// which is what lets the intent's items move once that Decomposition firing
// has closed. It advances no count: the count advanced at the open.
func (i *Intake) ClearReDecomposing(ctx context.Context, actor record.Actor, intentID string) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	return i.write(ctx, intentID, "closing the re-decomposition of", func(ctx context.Context, tx pgx.Tx, in Intent) error {
		if in.State != StateReDecomposing {
			return fmt.Errorf("%w: %s is %s", ErrNotReDecomposing, in.ID, in.State)
		}
		_, err := tx.Exec(ctx, `update `+Table+` set state = $1 where id = $2`, string(StateRefined), in.ID)
		return err
	})
}

// Escalate writes escalated where one of the intent's two counts exceeds the
// attempt limit. The limit is the caller's argument and is not read here: it is
// authored with gate policy and changes, and this package holds no policy.
// The value is written rather than recomputed from either count, because a
// decision read back against a value that was not in force when it was taken is
// not the decision that was taken.
//
// Which of the two counts exceeded the limit is what tells an escalated
// interview from an escalated decomposition, and it is read off the counts
// rather than off the value.
//
// An escalated intent waits on a human, so the notifier is told once the value
// is written, the way [Intake.Ask] tells it about a round.
func (i *Intake) Escalate(ctx context.Context, actor record.Actor, intentID string, limit int) (Intent, error) {
	if err := actor.Validate(); err != nil {
		return Intent{}, err
	}
	if limit <= 0 {
		return Intent{}, fmt.Errorf("%w: %d", ErrLimitNotPositive, limit)
	}
	if i.notifier == nil {
		return Intent{}, fmt.Errorf("%w: escalating %s", ErrNotifierNotComposed, intentID)
	}
	var escalated Intent
	err := i.write(ctx, intentID, "escalating", func(ctx context.Context, tx pgx.Tx, in Intent) error {
		if finished(in.State) {
			return fmt.Errorf("%w: %s is %s", ErrFinished, in.ID, in.State)
		}
		if in.Rounds <= limit && in.ReDecompositions <= limit {
			return fmt.Errorf("%w: %s stands at %d rounds and %d re-decompositions against a limit of %d",
				ErrLimitNotExceeded, in.ID, in.Rounds, in.ReDecompositions, limit)
		}
		if _, err := tx.Exec(ctx, `update `+Table+` set state = $1 where id = $2`,
			string(StateEscalated), in.ID); err != nil {
			return err
		}
		in.State = StateEscalated
		escalated = in
		return nil
	})
	if err != nil {
		return Intent{}, err
	}
	if err := i.notifier.Escalated(ctx, escalated.ID); err != nil {
		return escalated, fmt.Errorf("intent: telling a human about the escalation of %s: %w", escalated.ID, err)
	}
	return escalated, nil
}

// Drop writes dropped: a human at Work ends the intent for good — an
// escalation nobody clears, or a conformance intent whose decomposition found
// nothing departing. A component is refused, because an end read from a
// component is not the decision this value records.
//
// Ending an intent ends its unmerged items with it, and that is the caller's
// second act: Work calls dispatch to write dropped on each in the same action,
// and this package writes no item.
func (i *Intake) Drop(ctx context.Context, actor record.Actor, intentID string) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if actor.Kind != record.KindHuman {
		return fmt.Errorf("%w: %s is a %s", ErrNotAHuman, actor.Key, actor.Kind)
	}
	return i.write(ctx, intentID, "dropping", func(ctx context.Context, tx pgx.Tx, in Intent) error {
		if finished(in.State) {
			return fmt.Errorf("%w: %s is %s", ErrFinished, in.ID, in.State)
		}
		_, err := tx.Exec(ctx, `update `+Table+` set state = $1 where id = $2`, string(StateDropped), in.ID)
		return err
	})
}
