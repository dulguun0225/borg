package decisionlog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrBoundaryEmpty is returned by [Writer.Truncate] for a [Cut] naming no
	// boundary, which would remove the head along with everything else.
	ErrBoundaryEmpty = errors.New("decisionlog: a truncation names the row that will be its checkpoint")
	// ErrBoundaryUnknown is returned by [Writer.Truncate] for a [Cut] whose
	// Boundary names no row.
	ErrBoundaryUnknown = errors.New("decisionlog: the truncation's boundary names no row")
	// ErrLegalHoldStands is returned by [Writer.Truncate] where a legal hold
	// reaches what the cut would remove. A truncation is refused wherever one
	// reaches, and the rows a cut removes are every subject's, so any hold
	// standing over the factory, a project or a service reaches them.
	ErrLegalHoldStands = errors.New("decisionlog: a legal hold stands, and a truncation is refused wherever one reaches")
	// ErrNoRetentionInForce is returned by [Writer.Truncate] for a [Cut] naming
	// no retention value. The row says the value it enforced, and a cut made
	// under no value enforces nothing: where an owner has authored none the log
	// is kept for the life of the install and there is nothing to enforce.
	ErrNoRetentionInForce = errors.New("decisionlog: a truncation enforces a retention value, and the cut names none")
	// ErrBoundaryInsideTheRetention is returned by [Writer.Truncate] for a [Cut]
	// whose boundary row is younger than the retention value reaches back. Every
	// row the cut removes is older than that boundary, so a boundary inside the
	// retention would remove rows the value in force keeps — and the row would
	// name a value the cut did not obey.
	ErrBoundaryInsideTheRetention = errors.New("decisionlog: the boundary is inside the retention, so the cut would remove rows the value in force keeps")
)

// Cut is what [Writer.Truncate] is given: who authored the retention value
// being enforced, the value itself in seconds, the id of the oldest row that
// will remain — the truncation's boundary — and the policy version and score
// version in force at the cut. It is marshalled as the truncation row's
// payload.
//
// The actor is the author of the value and not whoever ran the pass, which is
// what the row is for: it says under what authority the rows went, and the pass
// itself is the factory enforcing a value a human authored.
type Cut struct {
	Actor record.Actor
	// RetentionSeconds is how long the value in force keeps a row, which is
	// what the boundary is checked against: a cut may remove a row older than
	// this and no row younger.
	RetentionSeconds int64
	Boundary         string
	PolicyVersion    string
	ScoreVersion     string
}

// Truncate enforces the log's retention: in one transaction under the
// advisory lock and the fence, it appends cut as a truncation row — naming
// the actor, the retention value, the boundary, and the versions in force —
// and then deletes every row with a lower sequence than the boundary row's.
// It refuses a boundary that names no row ([ErrBoundaryUnknown]) or that
// names none at all ([ErrBoundaryEmpty]), which would remove the head along
// with everything before it.
//
// The boundary is checked against the value: a cut removes every row before the
// boundary row, so the boundary's own timestamp is compared with the moment the
// retention reaches back to and a boundary inside it is refused with
// [ErrBoundaryInsideTheRetention]. A cut naming no value at all is
// [ErrNoRetentionInForce]. Both checks are here rather than in the caller
// because this is where the row that asserts the value and the row the cut
// stops at are both in hand. Cutting less than the value permits is not
// refused: what a shorter cut keeps is evidence, and the value bounds what may
// go rather than what must.
//
// legalHolds is every legal hold standing over the factory, a project or a
// service, each named in the words a reader sees, and one of them refuses the
// cut with [ErrLegalHoldStands]: while a legal hold stands, truncation is
// refused wherever it reaches, and a cut removes rows about every subject. The
// caller reads them — package legalhold's Standing is the read — because the
// package that owns that record may not be imported here: it is a record of the
// graph and this package is what every record package's writer appends through.
//
// [Reader.Verify] is what reads the boundary back afterwards: the oldest row
// remaining is the new checkpoint, and its prev_hash need not be empty
// because this truncation row names it.
func (w *Writer) Truncate(ctx context.Context, cut Cut, legalHolds []string) (Row, error) {
	if err := cut.Actor.Validate(); err != nil {
		return Row{}, err
	}
	if len(legalHolds) > 0 {
		return Row{}, fmt.Errorf("%w: %v", ErrLegalHoldStands, legalHolds)
	}
	if cut.Boundary == "" {
		return Row{}, ErrBoundaryEmpty
	}
	if cut.RetentionSeconds <= 0 {
		return Row{}, fmt.Errorf("%w: %d second(s)", ErrNoRetentionInForce, cut.RetentionSeconds)
	}
	// The moment the value in force reaches back to, read once for this cut:
	// every row the cut removes is older than the boundary, so a boundary at or
	// before this is a cut the value permits.
	reachesBackTo := record.FormatTime(time.Now().Add(-time.Duration(cut.RetentionSeconds) * time.Second))
	payload, err := json.Marshal(cut)
	if err != nil {
		return Row{}, fmt.Errorf("decisionlog: marshalling the cut: %w", err)
	}
	entry := Entry{
		Actor:         cut.Actor,
		Payload:       string(payload),
		FormatVersion: "truncation/1",
		PolicyVersion: cut.PolicyVersion,
		ScoreVersion:  cut.ScoreVersion,
	}
	if entry.PolicyVersion == "" || entry.ScoreVersion == "" {
		return Row{}, fmt.Errorf("%w: policy %q, score %q", ErrVersionsMissing, entry.PolicyVersion, entry.ScoreVersion)
	}

	var row Row
	err = withAppendTx(ctx, w.pool, w.token, func(ctx context.Context, tx pgx.Tx) error {
		var boundarySeq int64
		var boundaryAt string
		err := tx.QueryRow(ctx, `select seq, at from `+Table+` where id = $1`,
			cut.Boundary).Scan(&boundarySeq, &boundaryAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %q", ErrBoundaryUnknown, cut.Boundary)
		}
		if err != nil {
			return fmt.Errorf("decisionlog: reading the boundary row: %w", err)
		}
		// Every timestamp is fixed width and always UTC, so comparing two as
		// text is comparing them as times.
		if boundaryAt > reachesBackTo {
			return fmt.Errorf("%w: the boundary was written at %s and %d second(s) reaches back to %s",
				ErrBoundaryInsideTheRetention, boundaryAt, cut.RetentionSeconds, reachesBackTo)
		}

		inserted, err := insertRowTx(ctx, tx, ShapeTruncation, "", entry)
		if err != nil {
			return err
		}
		row = inserted

		if _, err := tx.Exec(ctx, `delete from `+Table+` where seq < $1`, boundarySeq); err != nil {
			return fmt.Errorf("decisionlog: deleting the rows the cut removes: %w", err)
		}
		return nil
	})
	if err != nil {
		return Row{}, err
	}
	return row, nil
}
