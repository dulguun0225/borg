package driftdetector

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/record"
)

// Head is the log's chain head this store recorded on the pass before this
// one: the second comparison's own anchor,
// ../../end-goal/deferred.md#security-comes-last seam 2's "the recorded head
// being the detector's own with nothing else reading it."
type Head struct {
	ID    string
	Actor record.Actor
	At    string
	Hash  string
	Seq   int64
}

// RecordHead overwrites the one row this store keeps of the chain's head.
// The pass calls it after [VerifyChain] finds the chain still holds the
// head it recorded, extended and nothing else — recording a head the chain
// disagreed with would make the next pass compare against a head this
// pass's own mismatch already impeaches.
func (w *Writer) RecordHead(ctx context.Context, hash string, seq int64) (Head, error) {
	h := Head{ID: record.NewID(HeadIDPrefix), Actor: Actor, At: record.Now(), Hash: hash, Seq: seq}
	_, err := w.pool.Exec(ctx, `insert into `+HeadTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, singleton, hash, seq)
		values ($1, $2, $3, $4, $5, $6, true, $7, $8)
		on conflict (singleton) do update set
			at = excluded.at, hash = excluded.hash, seq = excluded.seq`,
		h.ID, FormatVersionHead, string(h.Actor.Kind), h.Actor.Key, string(h.Actor.Basis), h.At, h.Hash, h.Seq)
	if err != nil {
		return Head{}, fmt.Errorf("driftdetector: recording the chain head: %w", err)
	}
	return h, nil
}

// GetHead is the recorded head, and false before the detector's first pass —
// nothing yet to verify the chain against.
func GetHead(ctx context.Context, pool *pgxpool.Pool) (Head, bool, error) {
	var h Head
	var kind, basis string
	err := pool.QueryRow(ctx, `select id, actor_kind, actor_key, actor_key_basis, at, hash, seq
		from `+HeadTable+` where singleton`).
		Scan(&h.ID, &kind, &h.Actor.Key, &basis, &h.At, &h.Hash, &h.Seq)
	if errors.Is(err, pgx.ErrNoRows) {
		return Head{}, false, nil
	} else if err != nil {
		return Head{}, false, fmt.Errorf("driftdetector: reading the chain head: %w", err)
	}
	h.Actor.Kind = record.Kind(kind)
	h.Actor.Basis = record.Basis(basis)
	return h, true, nil
}

// logRow is the columns of decisionlog's own table this package selects
// directly, in the order decisionlog.Row.ChainHash hashes them. This
// package reads the factory's decision_log table with a query of its own
// rather than through decisionlog.Reader, which would append a read event
// this package holds no fencing token to write — it is not a factory
// component and the factory's lease is not its to fence with.
const selectLogRows = `select seq, id, format_version, actor_kind, actor_key, actor_key_basis, at,
	shape, payload, policy_version, score_version, part, closes, verdict, reason,
	opened_in_work_at, self_approval, prev_hash, hash
	from ` + decisionlog.Table + ` where seq > $1 order by seq`

func scanLogRow(row pgx.Row) (decisionlog.Row, error) {
	var r decisionlog.Row
	var kind, basis string
	err := row.Scan(&r.Seq, &r.ID, &r.FormatVersion, &kind, &r.Actor.Key, &basis, &r.At,
		&r.Shape, &r.Payload, &r.PolicyVersion, &r.ScoreVersion, &r.Part, &r.Closes, &r.Verdict, &r.Reason,
		&r.OpenedInWorkAt, &r.SelfApproval, &r.PrevHash, &r.Hash)
	if err != nil {
		return decisionlog.Row{}, err
	}
	r.Actor.Kind = record.Kind(kind)
	r.Actor.Basis = record.Basis(basis)
	return r, nil
}

// checkpointRow is the one row named by seq, read to confirm the recorded
// head's own row is still what it was last pass — the case a truncation or
// an edit in place of that exact row leaves, which walking forward from it
// alone would never see, because there would be nothing after it to
// disagree.
func checkpointRow(ctx context.Context, pool *pgxpool.Pool, seq int64) (decisionlog.Row, bool, error) {
	r, err := scanLogRow(pool.QueryRow(ctx, `select seq, id, format_version, actor_kind, actor_key,
		actor_key_basis, at, shape, payload, policy_version, score_version, part, closes, verdict, reason,
		opened_in_work_at, self_approval, prev_hash, hash from `+decisionlog.Table+` where seq = $1`, seq))
	if errors.Is(err, pgx.ErrNoRows) {
		return decisionlog.Row{}, false, nil
	} else if err != nil {
		return decisionlog.Row{}, false, fmt.Errorf("driftdetector: reading the row at %d: %w", seq, err)
	}
	return r, true, nil
}

// VerifyChain is the second comparison: it reads the log's newest rows past
// the head this store recorded last pass, and confirms the chain still
// holds that head, extended and nothing else. A log with nothing recorded
// yet — the detector's first pass — has nothing to verify, and the newest
// row becomes the head to record.
//
// mismatch is true and why explains it where the row the recorded head
// named no longer carries that hash, or where a row after it fails to name
// its predecessor's stored hash or fails to hash to its own stored hash —
// the same two ways decisionlog's own chain breaks, over the same fields,
// recomputed here because that package's [decisionlog.Reader] is not this
// package's to call.
func VerifyChain(ctx context.Context, ownPool, factoryPool *pgxpool.Pool) (newHead Head, mismatch bool, why string, err error) {
	recorded, hadRecorded, err := GetHead(ctx, ownPool)
	if err != nil {
		return Head{}, false, "", err
	}

	if hadRecorded {
		row, found, err := checkpointRow(ctx, factoryPool, recorded.Seq)
		if err != nil {
			return Head{}, false, "", err
		}
		if !found || row.Hash != recorded.Hash {
			return recorded, true, fmt.Sprintf(
				"the row at sequence %d this store recorded as the chain's head is no longer there with the same hash",
				recorded.Seq), nil
		}
		// The stored hash column surviving untouched is not enough: an edit
		// of the row's other stored fields — its payload above all — leaves
		// that column exactly as it was and only recomputing from the row's
		// current fields catches it, the same check every row after the
		// checkpoint gets below.
		if computed := row.ChainHash(); computed != row.Hash {
			return recorded, true, fmt.Sprintf(
				"row %d (%s) stores hash %q, its fields hash to %q", row.Seq, row.ID, row.Hash, computed), nil
		}
	}

	rows, err := factoryPool.Query(ctx, selectLogRows, recorded.Seq)
	if err != nil {
		return Head{}, false, "", fmt.Errorf("driftdetector: reading the log past its recorded head: %w", err)
	}
	defer rows.Close()

	newest := recorded
	prevHash := recorded.Hash
	found := false
	for rows.Next() {
		row, err := scanLogRow(rows)
		if err != nil {
			return Head{}, false, "", fmt.Errorf("driftdetector: reading a row of the log: %w", err)
		}
		if hadRecorded || found {
			if row.PrevHash != prevHash {
				return recorded, true, fmt.Sprintf(
					"row %d (%s) names predecessor hash %q, the chain recorded last pass requires %q",
					row.Seq, row.ID, row.PrevHash, prevHash), nil
			}
		}
		if computed := row.ChainHash(); computed != row.Hash {
			return recorded, true, fmt.Sprintf(
				"row %d (%s) stores hash %q, its fields hash to %q", row.Seq, row.ID, row.Hash, computed), nil
		}
		prevHash = row.Hash
		newest = Head{Hash: row.Hash, Seq: row.Seq}
		found = true
	}
	if err := rows.Err(); err != nil {
		return Head{}, false, "", fmt.Errorf("driftdetector: reading the log past its recorded head: %w", err)
	}
	return newest, false, "", nil
}
