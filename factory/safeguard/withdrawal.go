package safeguard

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

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
// human the safeguard's own [Routing] names — that row is not built, so this
// and [ApproveWithdrawal] are the two writes it will call, in two separate
// transactions around the human's decision.
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
// the gate row that decides it, at its close — this package does not fire
// that row and takes no verdict, so a caller today is standing in for a row
// that does not exist yet. A withdrawal already approved is refused with
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

// Withdraw is [InsertWithdrawal] followed at once by [ApproveWithdrawal],
// standing in for the gate row A safeguard's withdrawal until that row
// exists: the row always holds a human, so nothing calls this in the shape
// the design wants outside a caller acting as that row until it is built.
// Package policy's Factory.WithdrawSafeguard is such a caller, and does not
// decide this — the row does, once it exists.
func Withdraw(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor, safeguardID string) error {
	safeguard, err := oneByID(ctx, tx, safeguardID)
	if err != nil {
		return err
	}
	w, err := InsertWithdrawal(ctx, tx, token, actor, safeguard.ID)
	if err != nil {
		return err
	}
	return ApproveWithdrawal(ctx, tx, token, w.ID)
}

// oneByID reads one safeguard by id inside tx, refusing [ErrNotFound] where
// none exists — [Withdraw] and its callers check this before writing a
// withdrawal naming a safeguard that was never placed.
func oneByID(ctx context.Context, tx pgx.Tx, id string) (Safeguard, error) {
	rows, err := tx.Query(ctx, selectSafeguards+` where id = $1`, id)
	if err != nil {
		return Safeguard{}, fmt.Errorf("safeguard: reading %s: %w", id, err)
	}
	defer rows.Close()
	if !rows.Next() {
		return Safeguard{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	p, err := scan(rows)
	if err != nil {
		return Safeguard{}, err
	}
	return p, rows.Err()
}
