package decisionlog

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Break is how a row breaks the chain.
type Break int

const (
	// BreakPredecessor is a row whose prev_hash is not the hash of the row
	// before it — or, for the first row, is not the empty string. A row
	// removed, inserted, or reordered from inside the chain breaks this way,
	// because a row after it still names what was there. A row removed from
	// the end does not: there is nothing after it to name it, and nothing
	// anchors the head. [Verify] states that limit.
	BreakPredecessor Break = iota + 1
	// BreakFields is a row whose stored hash is not the hash of its own
	// fields. A row edited in place breaks this way, and its successors do
	// not, because their prev_hash still names its unchanged stored hash.
	BreakFields
)

// BrokenError is what [Verify] returns for the first row that breaks the
// chain. Row is that row as it is stored, Break is how it breaks, and Want is
// the hash the chain requires where the row has something else.
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

// Verify walks the log in row order and returns the first row that breaks the
// chain, as a [*BrokenError], or nil for a log that is whole. An empty log is
// whole. It takes the pool and not a [Writer], because reading whether the
// chain is whole is not a reason to be handed the thing that appends to it.
//
// Each row is checked twice: that it names its predecessor's stored hash, and
// that its own stored hash is the hash of its fields. The predecessor check
// comes first, so a row that is both misplaced and edited is reported as
// misplaced. Row order is seq, which has gaps wherever an append rolled back;
// Verify requires the order and not the contiguity, because a gap is a
// transaction that never committed and not a row that was removed.
//
// # What Verify does not catch
//
// Rows removed from the end of the log. Verify walks forward from the empty
// predecessor hash and stops at whatever row is last, so a truncated tail is
// not there to be checked and Verify returns nil. Deleting the last row also
// frees its prev_hash value for the unique constraint, so an ordinary append
// then writes a replacement that verifies clean: the chain is whole and it is
// not the history that happened.
//
// That is the design's gap and not an oversight of this code. Seam 2 of
// "Security comes last" defers it — "Where the head is anchored and who reads
// it can wait" — ../../end-goal/deferred.md#security-comes-last. Anchoring the
// head is what closes it: until the highest row's hash is recorded somewhere
// the log's writer cannot reach, Verify proves that the rows present form one
// unbroken chain and does not prove that they are all of them.
// TestATruncatedTailIsNotCaught holds that limit in place, so removing the
// anchoring work later takes deleting a test that says what it is for.
//
// Verify reads the log a row at a time and holds one row, so a long log costs
// the query and not the memory.
func Verify(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx, selectRows)
	if err != nil {
		return fmt.Errorf("decisionlog: reading the log: %w", err)
	}
	defer rows.Close()

	prevHash := ""
	for rows.Next() {
		row, err := scan(rows)
		if err != nil {
			return err
		}
		if row.PrevHash != prevHash {
			return &BrokenError{Row: row, Break: BreakPredecessor, Want: prevHash}
		}
		if computed := row.ChainHash(); computed != row.Hash {
			return &BrokenError{Row: row, Break: BreakFields, Want: computed}
		}
		prevHash = row.Hash
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("decisionlog: reading the log: %w", err)
	}
	return nil
}
