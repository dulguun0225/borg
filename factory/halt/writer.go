package halt

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
	// ErrReasonEmpty is returned for a halt naming no reason.
	ErrReasonEmpty = errors.New("halt: a halt names the reason it was set for")
	// ErrHaltIDEmpty is returned by [InsertWithdrawal] for a withdrawal naming
	// no halt.
	ErrHaltIDEmpty = errors.New("halt: a withdrawal names the halt it ends")
	// ErrWithdrawalNotFound is returned by [ApproveWithdrawal] where no
	// withdrawal has the id.
	ErrWithdrawalNotFound = errors.New("halt: no withdrawal has that id")
	// ErrAlreadyApproved is returned by [ApproveWithdrawal] for a withdrawal
	// already approved.
	ErrAlreadyApproved = errors.New("halt: this withdrawal is already approved")
)

// Halt is one halt as it is stored: the record whose subject is the factory,
// naming the actor who set it and why. It is never edited.
type Halt struct {
	ID     string
	Actor  record.Actor
	At     string
	Reason string
}

// Withdrawal is a halt's withdrawal as it is stored: a second record naming
// the halt it ends, written pending and marked approved by a second write.
type Withdrawal struct {
	ID         string
	Actor      record.Actor
	At         string
	HaltID     string
	Approved   bool
	ApprovedAt string
}

// Writer is the table's one writer: Factory. It wraps the tx-taking calls
// below with a pool and a token for a caller that does not already hold a
// transaction of its own — package policy's own writes call the tx-taking
// calls directly, inside the transaction that appends the policy version.
type Writer struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewWriter returns the writer over pool, fencing every write with token.
func NewWriter(pool *pgxpool.Pool, token lease.Token) *Writer {
	return &Writer{pool: pool, token: token}
}

// Insert sets a halt, in its own transaction. Setting one is the owner's, and
// appending the policy version beside it is package policy's when it wires
// this in; this method is what a caller with no transaction of its own uses,
// tests among them.
func (w *Writer) Insert(ctx context.Context, actor record.Actor, reason string) (Halt, error) {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return Halt{}, fmt.Errorf("halt: beginning: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	h, err := Insert(ctx, tx, w.token, actor, reason)
	if err != nil {
		return Halt{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Halt{}, fmt.Errorf("halt: committing: %w", err)
	}
	return h, nil
}

// InsertWithdrawal writes a pending withdrawal, in its own transaction. See
// [Writer.Insert] for why a wrapper exists.
func (w *Writer) InsertWithdrawal(ctx context.Context, actor record.Actor, haltID string) (Withdrawal, error) {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return Withdrawal{}, fmt.Errorf("halt: beginning: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	wd, err := InsertWithdrawal(ctx, tx, w.token, actor, haltID)
	if err != nil {
		return Withdrawal{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Withdrawal{}, fmt.Errorf("halt: committing: %w", err)
	}
	return wd, nil
}

// ApproveWithdrawal marks one withdrawal approved, in its own transaction.
// See [Writer.Insert] for why a wrapper exists.
func (w *Writer) ApproveWithdrawal(ctx context.Context, withdrawalID string) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("halt: beginning: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := ApproveWithdrawal(ctx, tx, w.token, withdrawalID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("halt: committing: %w", err)
	}
	return nil
}

// Insert writes one halt inside tx. Its caller is package policy, appending
// the policy version in the same transaction, once policy wires this in —
// see doc.go for what is not built yet.
func Insert(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor, reason string) (Halt, error) {
	if err := lease.Fence(ctx, tx, token); err != nil {
		return Halt{}, err
	}
	if err := actor.Validate(); err != nil {
		return Halt{}, err
	}
	if reason == "" {
		return Halt{}, ErrReasonEmpty
	}
	h := Halt{ID: record.NewID(IDPrefix), Actor: actor, At: record.Now(), Reason: reason}
	_, err := tx.Exec(ctx, `insert into `+Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, reason)
		values ($1, $2, $3, $4, $5, $6, $7)`,
		h.ID, FormatVersion, string(h.Actor.Kind), h.Actor.Key, string(h.Actor.Basis), h.At, h.Reason,
	)
	if err != nil {
		return Halt{}, fmt.Errorf("halt: setting a halt: %w", err)
	}
	return h, nil
}

// InsertWithdrawal writes a pending withdrawal naming haltID, inside tx. It is
// not in force until [ApproveWithdrawal] marks it: the gate row A halt's
// withdrawal decides it, held by a human always and routed to the owner, the
// halt's subject being the factory. That row is
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/11-a-halts-withdrawal.md,
// which is not built, so this and [ApproveWithdrawal] are the two writes it
// will call, in two separate transactions around the human's decision.
func InsertWithdrawal(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor,
	haltID string) (Withdrawal, error) {
	if err := lease.Fence(ctx, tx, token); err != nil {
		return Withdrawal{}, err
	}
	if err := actor.Validate(); err != nil {
		return Withdrawal{}, err
	}
	if haltID == "" {
		return Withdrawal{}, ErrHaltIDEmpty
	}
	w := Withdrawal{ID: record.NewID(WithdrawalIDPrefix), Actor: actor, At: record.Now(), HaltID: haltID}
	_, err := tx.Exec(ctx, `insert into `+WithdrawalTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, halt_id, approved, approved_at)
		values ($1, $2, $3, $4, $5, $6, $7, false, null)`,
		w.ID, FormatVersionWithdrawal, string(w.Actor.Kind), w.Actor.Key, string(w.Actor.Basis), w.At, haltID,
	)
	if err != nil {
		return Withdrawal{}, fmt.Errorf("halt: writing a withdrawal of %s: %w", haltID, err)
	}
	return w, nil
}

// ApproveWithdrawal marks one withdrawal approved, inside tx. A withdrawal
// already approved is refused with [ErrAlreadyApproved].
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
		return fmt.Errorf("halt: reading withdrawal %s: %w", withdrawalID, err)
	}
	if approved {
		return fmt.Errorf("%w: %s", ErrAlreadyApproved, withdrawalID)
	}
	if _, err := tx.Exec(ctx, `update `+WithdrawalTable+` set approved = true, approved_at = $2 where id = $1`,
		withdrawalID, record.Now()); err != nil {
		return fmt.Errorf("halt: approving withdrawal %s: %w", withdrawalID, err)
	}
	return nil
}

const selectHalts = `select id, actor_kind, actor_key, actor_key_basis, at, reason from ` + Table

// Standing is every halt in force: every row [WithdrawalTable] names no
// approved withdrawal for. While any stands, every firing of a deploy to
// production row on every service holds and the merge queue stops
// fast-forwarding every service's candidates.
func Standing(ctx context.Context, pool *pgxpool.Pool) ([]Halt, error) {
	rows, err := pool.Query(ctx, selectHalts+`
		where not exists (select 1 from `+WithdrawalTable+` w where w.halt_id = `+Table+`.id and w.approved)
		order by at, id`)
	if err != nil {
		return nil, fmt.Errorf("halt: reading the halts in force: %w", err)
	}
	defer rows.Close()

	var read []Halt
	for rows.Next() {
		var h Halt
		var kind, basis string
		if err := rows.Scan(&h.ID, &kind, &h.Actor.Key, &basis, &h.At, &h.Reason); err != nil {
			return nil, fmt.Errorf("halt: reading a halt: %w", err)
		}
		h.Actor.Kind = record.Kind(kind)
		h.Actor.Basis = record.Basis(basis)
		read = append(read, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("halt: reading the halts in force: %w", err)
	}
	return read, nil
}
