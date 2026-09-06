package safeguard

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// ErrAlreadyApproved is returned by [ApproveWithdrawal] for a withdrawal
// already approved: a second approval is a second decision on the same row,
// which the gate that decides it never makes twice.
var ErrAlreadyApproved = errors.New("safeguard: this withdrawal is already approved")

// ErrWithdrawalNotFound is returned by [ApproveWithdrawal] where no withdrawal
// has the id.
var ErrWithdrawalNotFound = errors.New("safeguard: no withdrawal has that id")

// Withdrawal is a safeguard's withdrawal as it is stored: a second record
// naming the safeguard it ends, written pending and marked approved by a
// second write. The safeguard it names is in force until an approved
// withdrawal exists — [BySubjects] and [All]'s [Safeguard.Withdrawn] both read
// this table rather than a field of the safeguard, because a safeguard is
// never edited.
type Withdrawal struct {
	ID          string
	Actor       record.Actor
	At          string
	SafeguardID string
	Approved    bool
	ApprovedAt  string
}

// InsertWithdrawal writes a pending withdrawal naming safeguardID, inside tx.
// It is not in force until [ApproveWithdrawal] marks it: the gate row A
// safeguard's withdrawal decides it, held by a human always and routed to the
// human the safeguard's own [Routing] names. This and [ApproveWithdrawal] are
// the two writes that row makes, in two separate transactions around the
// human's decision.
func InsertWithdrawal(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor,
	safeguardID string) (Withdrawal, error) {
	if err := lease.Fence(ctx, tx, token); err != nil {
		return Withdrawal{}, err
	}
	if err := actor.Validate(); err != nil {
		return Withdrawal{}, err
	}
	if safeguardID == "" {
		return Withdrawal{}, ErrSubjectIDEmpty
	}
	w := Withdrawal{
		ID:          record.NewID(WithdrawalIDPrefix),
		Actor:       actor,
		At:          record.Now(),
		SafeguardID: safeguardID,
	}
	_, err := tx.Exec(ctx, `insert into `+WithdrawalTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, safeguard_id, approved, approved_at)
		values ($1, $2, $3, $4, $5, $6, $7, false, null)`,
		w.ID, FormatVersionWithdrawal, string(w.Actor.Kind), w.Actor.Key, string(w.Actor.Basis), w.At, safeguardID,
	)
	if err != nil {
		return Withdrawal{}, fmt.Errorf("safeguard: writing a withdrawal of %s: %w", safeguardID, err)
	}
	return w, nil
}

// ApproveWithdrawal marks one withdrawal approved, inside tx. Its caller is
// the gate row that decides it, at its close, reached through
// policy.Factory.ApproveSafeguardWithdrawal: this package does not fire that
// row and takes no verdict. A withdrawal already approved is refused with
// [ErrAlreadyApproved], the same rule the log's own close events take against
// a second ending.
func ApproveWithdrawal(ctx context.Context, tx pgx.Tx, token lease.Token, withdrawalID string) error {
	if err := lease.Fence(ctx, tx, token); err != nil {
		return err
	}
	var approved bool
	if err := tx.QueryRow(ctx, `select approved from `+WithdrawalTable+` where id = $1 for update`,
		withdrawalID).Scan(&approved); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrWithdrawalNotFound, withdrawalID)
		}
		return fmt.Errorf("safeguard: reading withdrawal %s: %w", withdrawalID, err)
	}
	if approved {
		return fmt.Errorf("%w: %s", ErrAlreadyApproved, withdrawalID)
	}
	if _, err := tx.Exec(ctx, `update `+WithdrawalTable+` set approved = true, approved_at = $2 where id = $1`,
		withdrawalID, record.Now()); err != nil {
		return fmt.Errorf("safeguard: approving withdrawal %s: %w", withdrawalID, err)
	}
	return nil
}

// GetWithdrawal is one withdrawal by id, read through the pool. The gate row
// that decides it reads it before it fires: the actor on this record is the one
// human the row may not route to, and the safeguard it names is what the row's
// own routing is read from.
func GetWithdrawal(ctx context.Context, pool *pgxpool.Pool, id string) (Withdrawal, error) {
	var w Withdrawal
	var kind, basis string
	var approvedAt *string
	err := pool.QueryRow(ctx, `select id, actor_kind, actor_key, actor_key_basis, at,
		safeguard_id, approved, approved_at from `+WithdrawalTable+` where id = $1`, id).
		Scan(&w.ID, &kind, &w.Actor.Key, &basis, &w.At, &w.SafeguardID, &w.Approved, &approvedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Withdrawal{}, fmt.Errorf("%w: %s", ErrWithdrawalNotFound, id)
		}
		return Withdrawal{}, fmt.Errorf("safeguard: reading withdrawal %s: %w", id, err)
	}
	w.Actor.Kind, w.Actor.Basis = record.Kind(kind), record.Basis(basis)
	if approvedAt != nil {
		w.ApprovedAt = *approvedAt
	}
	return w, nil
}

// There is no call here that writes a withdrawal and approves it in one step.
// A withdrawal is decided at the gate row A safeguard's withdrawal, held by a
// human always and routed away from whoever wrote it, so a caller that combined
// the two writes would be the mechanism that row exists to refuse: a record
// removing a human from a gate with no decision on it.
