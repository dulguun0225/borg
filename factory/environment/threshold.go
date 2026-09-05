package environment

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// SetGateThreshold writes the threshold an owner authored for one gate row on
// one environment, inside tx. Its one caller is package policy, which calls it
// inside the transaction that appends the policy version, so the row and the
// version commit together or not at all.
//
// Re-authoring is one row: the insert conflicts on the environment and the gate
// row and updates the threshold, leaving the row's actor and time at the first
// authoring. What that costs is that who last moved a threshold is not on the
// row; what says it is the policy version the write appended.
func SetGateThreshold(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor, environmentID, gateRow string, threshold float64) error {
	if err := lease.Fence(ctx, tx, token); err != nil {
		return err
	}
	if err := actor.Validate(); err != nil {
		return err
	}
	if gateRow == "" {
		return ErrGateRowEmpty
	}
	if threshold < 0 || threshold > 1 {
		return fmt.Errorf("%w: %v", ErrThresholdOutOfRange, threshold)
	}
	// The kind is read inside the same transaction as the write, because what
	// makes a candidate's record unauthorable is the record and not the caller:
	// an owner naming one is refused here rather than left to a reader to notice
	// a threshold nothing will ever compare against.
	var kind string
	err := tx.QueryRow(ctx, `select kind from `+Table+` where id = $1`, environmentID).Scan(&kind)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrNotFound, environmentID)
	} else if err != nil {
		return fmt.Errorf("environment: reading the kind of %s: %w", environmentID, err)
	}
	if Kind(kind) == KindCandidate {
		return fmt.Errorf("%w: %s is a candidate's", ErrNotAnOwnersKind, environmentID)
	}
	_, err = tx.Exec(ctx, `insert into `+ThresholdTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, environment_id, gate_row, threshold)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		on conflict (environment_id, gate_row) do update set threshold = excluded.threshold`,
		record.NewID(ThresholdIDPrefix), FormatVersionThreshold, string(actor.Kind), actor.Key, string(actor.Basis), record.Now(),
		environmentID, gateRow, threshold,
	)
	if err != nil {
		return fmt.Errorf("environment: authoring the %s threshold on %s: %w", gateRow, environmentID, err)
	}
	return nil
}

// GateThreshold is the threshold an owner authored for one gate row on one
// environment, and absent where they authored none — where the value in force is
// what the score supplies.
func GateThreshold(ctx context.Context, pool *pgxpool.Pool, environmentID, gateRow string) (gatepolicy.Authored, error) {
	var threshold float64
	err := pool.QueryRow(ctx, `select threshold from `+ThresholdTable+`
		where environment_id = $1 and gate_row = $2`, environmentID, gateRow).Scan(&threshold)
	if errors.Is(err, pgx.ErrNoRows) {
		return gatepolicy.Authored{}, nil
	} else if err != nil {
		return gatepolicy.Authored{}, fmt.Errorf("environment: reading the %s threshold of %s: %w", gateRow, environmentID, err)
	}
	return gatepolicy.Authored{Number: threshold, Present: true}, nil
}
