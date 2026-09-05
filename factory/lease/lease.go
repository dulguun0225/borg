package lease

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

// Token is the fencing token: the lease's number at the moment it was
// acquired or renewed. A holder carries it into every write it makes to the
// store, and [Fence] is what refuses a write whose token is not the lease's
// current number.
type Token int64

var (
	// ErrHeld is returned by [Acquire] when the lease is unexpired and held
	// by an instance other than the one asking.
	ErrHeld = errors.New("lease: held by another instance")
	// ErrFenced is returned by [Renew] and by [Fence] when a token is not
	// the lease's current number: a resumed instance that let its lease
	// lapse finds its first write refused this way, rather than acting
	// beside whoever holds the lease now.
	ErrFenced = errors.New("lease: token is not the lease's current number")
	// ErrNoLease is returned by [Fence] when the table holds no row yet —
	// a write attempted before anything has ever called [Acquire].
	ErrNoLease = errors.New("lease: no lease row")
)

// expired is the expiry an absent lease is seeded with: far enough in the
// past that the first call to [Acquire] always finds it lapsed, whichever
// instance asks.
var expired = record.FormatTime(time.Unix(0, 0))

// Acquire takes the lease for instance, or reports [ErrHeld] naming the
// current holder if another instance holds it unexpired. It seeds the single
// row where none exists yet, so the first call in a fresh store always
// succeeds. Where the row is absent, expired, or already held by instance,
// it sets the holder to instance, extends expires_at by ttl from now, raises
// number by one, and returns the new number as a [Token] — reacquiring by
// the instance that already holds it is still a new token, the way a renewal
// through [Renew] is not.
func Acquire(ctx context.Context, pool *pgxpool.Pool, instance string, ttl time.Duration) (Token, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("lease: beginning: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `insert into `+Table+` (id, instance, expires_at, number)
		values (1, '', $1, 0)
		on conflict (id) do nothing`, expired); err != nil {
		return 0, fmt.Errorf("lease: seeding the row: %w", err)
	}

	var holder, expiresAt string
	var number int64
	if err := tx.QueryRow(ctx, `select instance, expires_at, number from `+Table+` where id = 1 for update`).
		Scan(&holder, &expiresAt, &number); err != nil {
		return 0, fmt.Errorf("lease: reading the row: %w", err)
	}

	expiry, err := record.ParseTime(expiresAt)
	if err != nil {
		return 0, fmt.Errorf("lease: parsing the stored expiry: %w", err)
	}
	if time.Now().Before(expiry) && holder != instance {
		return 0, fmt.Errorf("%w: %q", ErrHeld, holder)
	}

	number++
	if _, err := tx.Exec(ctx, `update `+Table+` set instance = $1, expires_at = $2, number = $3 where id = 1`,
		instance, record.FormatTime(time.Now().Add(ttl)), number); err != nil {
		return 0, fmt.Errorf("lease: updating: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("lease: committing: %w", err)
	}
	return Token(number), nil
}

// Renew extends the lease's expiry by ttl from now, refusing with [ErrFenced]
// unless token is still the lease's current number — a stale holder's renewal
// is refused the same way its next write would be.
func Renew(ctx context.Context, pool *pgxpool.Pool, token Token, ttl time.Duration) error {
	tag, err := pool.Exec(ctx, `update `+Table+` set expires_at = $1 where id = 1 and number = $2`,
		record.FormatTime(time.Now().Add(ttl)), int64(token))
	if err != nil {
		return fmt.Errorf("lease: renewing: %w", err)
	}
	if tag.RowsAffected() != 0 {
		return nil
	}
	var current int64
	if err := pool.QueryRow(ctx, `select number from `+Table+` where id = 1`).Scan(&current); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoLease
		}
		return fmt.Errorf("%w: token %d, and the current number could not be read: %v", ErrFenced, token, err)
	}
	return fmt.Errorf("%w: current number %d, token %d", ErrFenced, current, token)
}

// Fence refuses a write whose token is not the lease's current number. Every
// writer in the module calls it inside its own write transaction, on tx,
// before the write it guards — so the two either commit together or the
// caller's rollback undoes neither. It returns [ErrNoLease] where the table
// holds no row, which a write attempted before anything has ever called
// [Acquire] is.
func Fence(ctx context.Context, tx pgx.Tx, token Token) error {
	var number int64
	err := tx.QueryRow(ctx, `select number from `+Table+` where id = 1 for share`).Scan(&number)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoLease
	}
	if err != nil {
		return fmt.Errorf("lease: reading the lease: %w", err)
	}
	if number != int64(token) {
		return fmt.Errorf("%w: current number %d, token %d", ErrFenced, number, token)
	}
	return nil
}
