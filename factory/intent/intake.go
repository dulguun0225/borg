package intent

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// Intake is the one writer of intents, of their questions, and of the
// requirements attached to them. The three sources are three callers of this
// one entrance, which is why every method takes the actor rather than the
// writer holding one.
type Intake struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewIntake returns the writer over pool, fencing every write with token.
func NewIntake(pool *pgxpool.Pool, token lease.Token) *Intake {
	return &Intake{pool: pool, token: token}
}

// intentColumns is the intent's stored fields in the order [scanIntent] reads
// them, written once so a select and its scan cannot drift apart.
const intentColumns = `id, actor_kind, actor_key, actor_key_basis, at, source, statement, state,
	rounds, re_decompositions, tier, tier_policy_version, project_id, intended_effect,
	evidence, deadline, constraint_id, sent_back_by, outcome`

// scanIntent reads one row of [intentColumns] into an [Intent].
func scanIntent(row pgx.Row) (Intent, error) {
	var in Intent
	var kind, basis, source, state, sentBackBy string
	err := row.Scan(&in.ID, &kind, &in.Actor.Key, &basis, &in.At, &source, &in.Statement, &state,
		&in.Rounds, &in.ReDecompositions, &in.Tier.Value, &in.Tier.PolicyVersion, &in.ProjectID,
		&in.IntendedEffect, &in.Evidence, &in.Deadline, &in.ConstraintID, &sentBackBy, &in.Outcome)
	if err != nil {
		return Intent{}, err
	}
	in.Actor.Kind = record.Kind(kind)
	in.Actor.Basis = record.Basis(basis)
	in.Source = Source(source)
	in.State = State(state)
	in.SentBackBy = SentBackBy(sentBackBy)
	return in, nil
}

// lockIntent reads the intent inside tx and holds its row for the
// transaction, which is what keeps two concurrent writes out of one count and
// one state.
func lockIntent(ctx context.Context, tx pgx.Tx, intentID string) (Intent, error) {
	if intentID == "" {
		return Intent{}, ErrIntentIDEmpty
	}
	in, err := scanIntent(tx.QueryRow(ctx, `select `+intentColumns+` from `+Table+` where id = $1 for update`, intentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Intent{}, fmt.Errorf("%w: %s", ErrIntentNotFound, intentID)
	} else if err != nil {
		return Intent{}, fmt.Errorf("intent: reading %s: %w", intentID, err)
	}
	return in, nil
}

// finished reports the states no write moves an intent out of.
func finished(state State) bool { return state == StateDropped || state == StateDelivered }

// Arrival is one intent as it arrives. Source and Statement are required and
// everything else is written where the source supplies it: nothing is judged
// on the way in, because judging it is what the interview is for.
type Arrival struct {
	Source    Source
	Statement string
	// ProjectID is the project the request came in under, where a source
	// supplies one. It is where decomposition places a service the work
	// creates, and it is not a claim about where the work is.
	ProjectID string
	// Evidence is what raised an intent the factory raised. It is required on
	// [SourceDetector] and refused on the other two.
	Evidence Evidence
	// ConstraintID is whatever constraint arrived with the request and binds
	// only what is decomposed from it.
	ConstraintID string
	// Tier is written at arrival only for a detector's intent, from the
	// constraint or the detector that raised it. For the other two sources it
	// is written at the confirming round instead.
	Tier Tier
	// Deadline is the trigger's own time plus a constraint's period, an
	// instant in UTC, where the arrival is itself the trigger.
	Deadline string
}

// TakeIn writes an intent as it arrives: unrefined, zero rounds, zero
// re-decompositions, and judged by nothing on the way in.
//
// The three asymmetries between an intent somebody asked for and one the
// factory raised are refused here and again by [DDL]: the factory's own
// carries evidence and the other two do not, the factory's own arrives with
// its tier and the other two are given one at the confirming round, and only
// the factory's own may be attached to by [OnEvidence].
func (i *Intake) TakeIn(ctx context.Context, actor record.Actor, arrival Arrival) (Intent, error) {
	if err := actor.Validate(); err != nil {
		return Intent{}, err
	}
	if !slices.Contains(Sources, arrival.Source) {
		return Intent{}, fmt.Errorf("%w: %q", ErrSourceUnknown, arrival.Source)
	}
	if arrival.Statement == "" {
		return Intent{}, ErrStatementEmpty
	}
	evidence, err := arrival.Evidence.Key()
	if err != nil {
		return Intent{}, err
	}
	if arrival.Source == SourceDetector && evidence == "" {
		return Intent{}, ErrEvidenceEmpty
	}
	if arrival.Source != SourceDetector && evidence != "" {
		return Intent{}, fmt.Errorf("%w: source %q", ErrEvidenceOnARequest, arrival.Source)
	}
	if (arrival.Tier.Value == 0) != (arrival.Tier.PolicyVersion == "") {
		return Intent{}, fmt.Errorf("%w: %+v", ErrTierIncomplete, arrival.Tier)
	}
	if arrival.Source != SourceDetector && arrival.Tier.Written() {
		return Intent{}, fmt.Errorf("%w: a tier is proposed at the confirming round", ErrRequesterOwed)
	}

	in := Intent{
		ID:           record.NewID(IDPrefix),
		Actor:        actor,
		At:           record.Now(),
		Source:       arrival.Source,
		Statement:    arrival.Statement,
		State:        StateUnrefined,
		Tier:         arrival.Tier,
		ProjectID:    arrival.ProjectID,
		Evidence:     evidence,
		Deadline:     arrival.Deadline,
		ConstraintID: arrival.ConstraintID,
	}

	tx, err := i.pool.Begin(ctx)
	if err != nil {
		return Intent{}, fmt.Errorf("intent: beginning the take-in of %s: %w", in.ID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, i.token); err != nil {
		return Intent{}, err
	}

	_, err = tx.Exec(ctx, `insert into `+Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, source, statement, state,
		 rounds, re_decompositions, tier, tier_policy_version, project_id, intended_effect,
		 evidence, deadline, constraint_id, sent_back_by, outcome)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, 0, 0, $10, $11, $12, '', $13, $14, $15, '', '')`,
		in.ID, FormatVersion, string(in.Actor.Kind), in.Actor.Key, string(in.Actor.Basis), in.At,
		string(in.Source), in.Statement, string(in.State),
		in.Tier.Value, in.Tier.PolicyVersion, in.ProjectID, in.Evidence, in.Deadline, in.ConstraintID,
	)
	if err != nil {
		return Intent{}, fmt.Errorf("intent: taking in %s: %w", in.ID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Intent{}, fmt.Errorf("intent: committing the take-in of %s: %w", in.ID, err)
	}
	return in, nil
}

// SetDeadline writes the deadline a constraint's trigger computes: the
// trigger's own time plus the constraint's period, an instant in UTC. It is
// written when the trigger occurs, which is later than the arrival for the
// two triggers that are records and for the human's mark at Work, and it is
// this package's one field computed from a calendar — computed by the caller,
// which holds the constraint and its time zone.
func (i *Intake) SetDeadline(ctx context.Context, actor record.Actor, intentID, deadline string) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if _, err := record.ParseTime(deadline); err != nil {
		return fmt.Errorf("intent: the deadline of %s: %w", intentID, err)
	}
	return i.write(ctx, intentID, "writing the deadline of", func(ctx context.Context, tx pgx.Tx, in Intent) error {
		if finished(in.State) {
			return fmt.Errorf("%w: %s is %s", ErrFinished, in.ID, in.State)
		}
		_, err := tx.Exec(ctx, `update `+Table+` set deadline = $1 where id = $2`, deadline, in.ID)
		return err
	})
}

// SetProject writes the project the work is placed in, once. Its caller is
// decomposition, and it is called for one case: decomposition has to place a
// service the work creates and nothing else answers where — no source supplied
// a project at the arrival, and there is no existing service to reach one
// through.
//
// It is never rewritten, so an approval keeps pointing at what was approved: a
// call on an intent that already names a project is [ErrProjectAlreadyWritten],
// whether it names the same project or another one. The refusal is here and not
// a CHECK, a column being unable to see what stood in it before the update; the
// row is locked and read in the same transaction, so two concurrent fills are
// one fill and one refusal.
func (i *Intake) SetProject(ctx context.Context, actor record.Actor, intentID, projectID string) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if projectID == "" {
		return ErrProjectIDEmpty
	}
	return i.write(ctx, intentID, "writing the project of", func(ctx context.Context, tx pgx.Tx, in Intent) error {
		if finished(in.State) {
			return fmt.Errorf("%w: %s is %s", ErrFinished, in.ID, in.State)
		}
		if in.ProjectID != "" {
			return fmt.Errorf("%w: %s is in %s", ErrProjectAlreadyWritten, in.ID, in.ProjectID)
		}
		_, err := tx.Exec(ctx, `update `+Table+` set project_id = $1 where id = $2`, projectID, in.ID)
		return err
	})
}

// write is the shape every write to an existing intent takes: a fenced
// transaction, the row locked and read, the caller's own change, and a
// commit. what names the write in the error text.
func (i *Intake) write(ctx context.Context, intentID, what string,
	change func(context.Context, pgx.Tx, Intent) error,
) error {
	tx, err := i.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("intent: beginning %s %s: %w", what, intentID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, i.token); err != nil {
		return err
	}
	in, err := lockIntent(ctx, tx, intentID)
	if err != nil {
		return err
	}
	if err := change(ctx, tx, in); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("intent: committing %s %s: %w", what, intentID, err)
	}
	return nil
}
