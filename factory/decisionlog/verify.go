package decisionlog

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Break is how a row breaks the chain.
type Break int

const (
	// BreakPredecessor is a row whose prev_hash is not the hash of the row
	// before it — or, for the first row remaining, is not the empty string
	// and is not named as a checkpoint by a truncation row still in the log.
	// A row removed, inserted, or reordered from inside the chain breaks
	// this way, because a row after it still names what was there. A row
	// removed from the end does not: there is nothing after it to name it,
	// and nothing anchors the head. [Reader.Verify] states that limit.
	BreakPredecessor Break = iota + 1
	// BreakFields is a row whose stored hash is not the hash of its own
	// fields. A row edited in place breaks this way, and its successors do
	// not, because their prev_hash still names its unchanged stored hash.
	BreakFields
)

// BrokenError is what [Reader.Verify] returns for the first row that breaks
// the chain. Row is that row as it is stored, Break is how it breaks, and
// Want is the hash the chain requires where the row has something else.
type BrokenError struct {
	Row   Row
	Break Break
	Want  string
}

func (e *BrokenError) Error() string {
	switch e.Break {
	case BreakPredecessor:
		return fmt.Sprintf("decisionlog: row %d (%s) names predecessor hash %q, the chain requires %q",
			e.Row.Seq, e.Row.ID, e.Row.PrevHash, e.Want)
	default:
		return fmt.Sprintf("decisionlog: row %d (%s) stores hash %q, its fields hash to %q",
			e.Row.Seq, e.Row.ID, e.Row.Hash, e.Want)
	}
}

// verify walks the log in row order and returns the first row that breaks
// the chain, as a [*BrokenError], or nil for a log that is whole. An empty
// log is whole.
//
// Each row is checked twice: that it names its predecessor's stored hash,
// and that its own stored hash is the hash of its fields. The predecessor
// check comes first, so a row that is both misplaced and edited is reported
// as misplaced. Row order is seq, which has gaps wherever an append rolled
// back; this requires the order and not the contiguity, because a gap is a
// transaction that never committed and not a row that was removed.
//
// The oldest row remaining is the chain's checkpoint. Ordinarily its
// prev_hash is empty, the first row's predecessor being nothing; where
// [Writer.Truncate] has run, a truncation row still in the log names that
// row's id as its cut's boundary, and its prev_hash is accepted as stored
// instead. Gathering the boundaries first is one query over the truncation
// rows alone — few next to the log they cut — so the walk that follows
// still holds one row at a time.
//
// # What verify does not catch
//
// Rows removed from the end of the log with no truncation row naming a
// remaining row as their boundary. verify walks forward from the checkpoint
// and stops at whatever row is last, so a tail truncated by hand — deleting
// rows directly rather than through [Writer.Truncate] — is not there to be
// checked and verify returns nil for it. That is seam 2's deferral and not a
// gap in this code:
// ../../end-goal/deferred.md#security-comes-last defers where the head is
// anchored to the drift detector's own store, which records the head each
// pass and verifies the chain still holds it, extended and nothing else.
// TestATruncatedTailIsNotCaughtByVerifyAlone holds that limit in place.
func verify(ctx context.Context, pool *pgxpool.Pool) error {
	boundaries, err := truncationBoundaries(ctx, pool)
	if err != nil {
		return err
	}

	rows, err := pool.Query(ctx, selectRows)
	if err != nil {
		return fmt.Errorf("decisionlog: reading the log: %w", err)
	}
	defer rows.Close()

	prevHash := ""
	first := true
	for rows.Next() {
		row, err := scan(rows)
		if err != nil {
			return err
		}
		want := prevHash
		if first && row.PrevHash != "" && boundaries[row.ID] {
			want = row.PrevHash
		}
		if row.PrevHash != want {
			return &BrokenError{Row: row, Break: BreakPredecessor, Want: want}
		}
		if computed := row.ChainHash(); computed != row.Hash {
			return &BrokenError{Row: row, Break: BreakFields, Want: computed}
		}
		prevHash = row.Hash
		first = false
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("decisionlog: reading the log: %w", err)
	}
	return nil
}

// truncationBoundaries is the set of row ids every truncation row still in
// the log names as its cut's boundary.
func truncationBoundaries(ctx context.Context, pool *pgxpool.Pool) (map[string]bool, error) {
	rows, err := pool.Query(ctx, `select payload from `+Table+` where shape = $1`, string(ShapeTruncation))
	if err != nil {
		return nil, fmt.Errorf("decisionlog: reading truncation rows: %w", err)
	}
	defer rows.Close()

	boundaries := make(map[string]bool)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("decisionlog: reading a truncation row: %w", err)
		}
		var cut Cut
		if err := json.Unmarshal([]byte(payload), &cut); err != nil {
			return nil, fmt.Errorf("decisionlog: reading a truncation row's cut: %w", err)
		}
		boundaries[cut.Boundary] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("decisionlog: reading truncation rows: %w", err)
	}
	return boundaries, nil
}
