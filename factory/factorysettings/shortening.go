package factorysettings

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrShorteningAlreadyApproved is returned by [ApproveShortening] for a
	// shortening already approved: a second approval is a second decision on the
	// same row, which the gate that decides it never makes twice.
	ErrShorteningAlreadyApproved = errors.New("factorysettings: this shortening is already approved")
	// ErrShorteningNotFound is returned where no shortening has the id.
	ErrShorteningNotFound = errors.New("factorysettings: no shortening of decision-log retention has that id")
)

// Shortening is a shorter decision-log retention value as it is stored: a
// record of its own naming who authored the value and what the value is,
// written pending and marked approved by a second write. It exists because the
// row that decides a shortening is routed away from whoever authored the value,
// and a row can only route away from an actor a record names — the same
// arrangement a safeguard's withdrawal has, for the same reason.
//
// The value is not in force until the row approves it: [Get] reads the field of
// the settings record, which the approval writes.
type Shortening struct {
	ID    string
	Actor record.Actor
	At    string
	// Seconds is the shorter value the row decides.
	Seconds int64
	// Approved is whether the gate row that decides it approved it, and
	// ApprovedAt is when.
	Approved   bool
	ApprovedAt string
}

// InsertShortening writes a pending shortening, inside tx. It is not in force
// until [ApproveShortening] marks it: the gate row that decides a shortening of
// decision-log retention decides it, held by a human always and routed away
// from the actor on this record. This and [ApproveShortening] are the two writes
// that row makes, in two separate transactions around the human's decision,
// through package policy's WriteRetentionShortening and
// ApproveRetentionShortening.
func InsertShortening(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor,
	seconds int64) (Shortening, error) {
	if err := lease.Fence(ctx, tx, token); err != nil {
		return Shortening{}, err
	}
	if err := actor.Validate(); err != nil {
		return Shortening{}, err
	}
	if seconds <= 0 {
		return Shortening{}, fmt.Errorf("%w: %d", ErrRetentionNotPositive, seconds)
	}
	s := Shortening{
		ID:      record.NewID(ShorteningIDPrefix),
		Actor:   actor,
		At:      record.Now(),
		Seconds: seconds,
	}
	_, err := tx.Exec(ctx, `insert into `+ShorteningTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, seconds, approved, approved_at)
		values ($1, $2, $3, $4, $5, $6, $7, false, null)`,
		s.ID, FormatVersionShortening, string(s.Actor.Kind), s.Actor.Key, string(s.Actor.Basis),
		s.At, s.Seconds)
	if err != nil {
		return Shortening{}, fmt.Errorf("factorysettings: writing a shortening to %d second(s): %w", seconds, err)
	}
	return s, nil
}

// ApproveShortening marks one shortening approved, inside tx. Its caller is the
// close of the gate row that decides it, reached through package policy's
// ApproveRetentionShortening, which writes the field in the same transaction.
// A shortening already approved is refused with [ErrShorteningAlreadyApproved].
func ApproveShortening(ctx context.Context, tx pgx.Tx, token lease.Token, shorteningID string) error {
	if err := lease.Fence(ctx, tx, token); err != nil {
		return err
	}
	var approved bool
	if err := tx.QueryRow(ctx, `select approved from `+ShorteningTable+` where id = $1 for update`,
		shorteningID).Scan(&approved); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrShorteningNotFound, shorteningID)
		}
		return fmt.Errorf("factorysettings: reading shortening %s: %w", shorteningID, err)
	}
	if approved {
		return fmt.Errorf("%w: %s", ErrShorteningAlreadyApproved, shorteningID)
	}
	if _, err := tx.Exec(ctx, `update `+ShorteningTable+` set approved = true, approved_at = $2 where id = $1`,
		shorteningID, record.Now()); err != nil {
		return fmt.Errorf("factorysettings: approving shortening %s: %w", shorteningID, err)
	}
	return nil
}

// GetShortening is one shortening by id, read through the pool. The gate row
// that decides it reads it before it fires: the actor on this record is the one
// human the row may not route to, and the value is what the row decides.
func GetShortening(ctx context.Context, pool *pgxpool.Pool, id string) (Shortening, error) {
	var s Shortening
	var kind, basis string
	var approvedAt *string
	err := pool.QueryRow(ctx, `select id, actor_kind, actor_key, actor_key_basis, at,
		seconds, approved, approved_at from `+ShorteningTable+` where id = $1`, id).
		Scan(&s.ID, &kind, &s.Actor.Key, &basis, &s.At, &s.Seconds, &s.Approved, &approvedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Shortening{}, fmt.Errorf("%w: %s", ErrShorteningNotFound, id)
		}
		return Shortening{}, fmt.Errorf("factorysettings: reading shortening %s: %w", id, err)
	}
	s.Actor.Kind, s.Actor.Basis = record.Kind(kind), record.Basis(basis)
	if approvedAt != nil {
		s.ApprovedAt = *approvedAt
	}
	return s, nil
}

// There is no call here that writes a shortening and approves it in one step. A
// shortening is decided at a gate row of its own, held by a human always and
// routed away from whoever authored the value, so a caller that combined the
// two writes would be the mechanism that row exists to refuse: evidence
// destroyed with no decision on it.
