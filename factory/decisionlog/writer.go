package decisionlog

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrVersionsMissing is returned by [Writer.AppendDecisionOpening] for an
	// entry naming no policy version or no score version.
	ErrVersionsMissing = errors.New("decisionlog: an opening names a policy version and a score version")
	// ErrVersionsRefused is returned by [Writer.AppendDecisionClosing],
	// [Writer.AppendPageEvent], and [Writer.AppendWait] for an entry naming
	// either version. A page event is a delivery and a wait is a wait, so
	// neither was decided under anything; a closing was, but what it was
	// decided under is written once, on the opening row it names.
	ErrVersionsRefused = errors.New("decisionlog: only an opening names a policy version or a score version")
	// ErrClosesMissing is returned by [Writer.AppendDecisionClosing] for an
	// entry naming no row to close.
	ErrClosesMissing = errors.New("decisionlog: a closing names the opening row it closes")
	// ErrClosesRefused is returned by the other three append methods for an
	// entry naming a row to close.
	ErrClosesRefused = errors.New("decisionlog: only a closing names a row it closes")
	// ErrNotAnOpening is returned by [Writer.AppendDecisionClosing] when the
	// row the entry names does not exist or is not an opening decision row.
	ErrNotAnOpening = errors.New("decisionlog: a closing closes an opening decision row")
)

// Writer is the log's one writer. Every component that decides anything holds
// one and appends through it; nothing else inserts into the table.
type Writer struct {
	pool *pgxpool.Pool
}

// NewWriter returns the writer over pool.
func NewWriter(pool *pgxpool.Pool) *Writer { return &Writer{pool: pool} }

// AppendDecisionOpening appends a decision's opening row, written when the
// gate fires. It names both versions and closes nothing.
func (w *Writer) AppendDecisionOpening(ctx context.Context, e Entry) (Row, error) {
	if e.PolicyVersion == "" || e.ScoreVersion == "" {
		return Row{}, fmt.Errorf("%w: policy %q, score %q", ErrVersionsMissing, e.PolicyVersion, e.ScoreVersion)
	}
	if err := refuseCloses("an opening", e); err != nil {
		return Row{}, err
	}
	return w.append(ctx, ShapeDecision, PartOpening, e)
}

// AppendDecisionClosing appends a decision's closing row, written when the
// verdict is given. It names the opening row it closes and neither version.
// It fails with [ErrNotAnOpening] when the named row does not exist or is not
// an opening decision row, and a second closing on one opening is refused by
// the store's partial unique index rather than by this method.
func (w *Writer) AppendDecisionClosing(ctx context.Context, e Entry) (Row, error) {
	if err := refuseVersions("a closing", e); err != nil {
		return Row{}, err
	}
	if e.Closes == "" {
		return Row{}, fmt.Errorf("%w: the entry names none", ErrClosesMissing)
	}
	return w.append(ctx, ShapeDecision, PartClosing, e)
}

// AppendPageEvent appends a page that was delivered, which names neither
// version and closes nothing.
func (w *Writer) AppendPageEvent(ctx context.Context, e Entry) (Row, error) {
	if err := refuseVersions("a page event", e); err != nil {
		return Row{}, err
	}
	if err := refuseCloses("a page event", e); err != nil {
		return Row{}, err
	}
	return w.append(ctx, ShapePageEvent, "", e)
}

// AppendWait appends something the factory could not compute when a gate
// fired, which names neither version and closes nothing.
func (w *Writer) AppendWait(ctx context.Context, e Entry) (Row, error) {
	if err := refuseVersions("a wait", e); err != nil {
		return Row{}, err
	}
	if err := refuseCloses("a wait", e); err != nil {
		return Row{}, err
	}
	return w.append(ctx, ShapeWait, "", e)
}

func refuseVersions(what string, e Entry) error {
	if e.PolicyVersion != "" || e.ScoreVersion != "" {
		return fmt.Errorf("%w: %s named policy %q, score %q", ErrVersionsRefused, what, e.PolicyVersion, e.ScoreVersion)
	}
	return nil
}

func refuseCloses(what string, e Entry) error {
	if e.Closes != "" {
		return fmt.Errorf("%w: %s named %q", ErrClosesRefused, what, e.Closes)
	}
	return nil
}

const insertRow = `insert into ` + Table + `
	(seq, id, actor_kind, actor_name, at, shape, payload, policy_version, score_version, part, closes, prev_hash, hash)
	values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`

// append is every append: take the lock, check what a closing names, read the
// head, take the next sequence value, hash, insert. The order matters — the
// lock is taken before the head is read, so no two transactions read the same
// head, and the closing's check runs under it, so what it found holds until
// the insert commits.
func (w *Writer) append(ctx context.Context, shape Shape, part Part, e Entry) (Row, error) {
	if err := e.Actor.Validate(); err != nil {
		return Row{}, err
	}

	tx, err := w.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Row{}, fmt.Errorf("decisionlog: beginning the append: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, AdvisoryLockKey); err != nil {
		return Row{}, fmt.Errorf("decisionlog: taking the append lock: %w", err)
	}

	if part == PartClosing {
		var closedShape, closedPart string
		err := tx.QueryRow(ctx, `select shape, part from `+Table+` where id = $1`, e.Closes).
			Scan(&closedShape, &closedPart)
		if errors.Is(err, pgx.ErrNoRows) {
			return Row{}, fmt.Errorf("%w: %q names no row", ErrNotAnOpening, e.Closes)
		} else if err != nil {
			return Row{}, fmt.Errorf("decisionlog: reading the row a closing names: %w", err)
		}
		if Shape(closedShape) != ShapeDecision || Part(closedPart) != PartOpening {
			return Row{}, fmt.Errorf("%w: %q is shape %q, part %q", ErrNotAnOpening, e.Closes, closedShape, closedPart)
		}
	}

	var prevHash string
	err = tx.QueryRow(ctx, `select hash from `+Table+` order by seq desc limit 1`).Scan(&prevHash)
	if errors.Is(err, pgx.ErrNoRows) {
		prevHash = "" // The first row's predecessor hash is the empty string.
	} else if err != nil {
		return Row{}, fmt.Errorf("decisionlog: reading the head: %w", err)
	}

	// The sequence name is a constant of this package and not input, so
	// writing it into the statement is not a place anything can be injected.
	// nextval takes a regclass, which is not a type a parameter carries.
	var seq int64
	if err := tx.QueryRow(ctx, `select nextval('`+Sequence+`')`).Scan(&seq); err != nil {
		return Row{}, fmt.Errorf("decisionlog: taking the next sequence value: %w", err)
	}

	row := Row{
		Seq:           seq,
		ID:            record.NewID(IDPrefix),
		Actor:         e.Actor,
		At:            record.Now(),
		Shape:         shape,
		Payload:       e.Payload,
		PolicyVersion: e.PolicyVersion,
		ScoreVersion:  e.ScoreVersion,
		Part:          part,
		Closes:        e.Closes,
		PrevHash:      prevHash,
	}
	row.Hash = row.ChainHash()

	if _, err := tx.Exec(ctx, insertRow,
		row.Seq, row.ID, string(row.Actor.Kind), row.Actor.Name, row.At,
		string(row.Shape), row.Payload, row.PolicyVersion, row.ScoreVersion,
		string(row.Part), row.Closes, row.PrevHash, row.Hash,
	); err != nil {
		return Row{}, fmt.Errorf("decisionlog: appending row %d: %w", row.Seq, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Row{}, fmt.Errorf("decisionlog: committing row %d: %w", row.Seq, err)
	}
	return row, nil
}

const selectRows = `select seq, id, actor_kind, actor_name, at, shape, payload,
	policy_version, score_version, part, closes, prev_hash, hash
	from ` + Table + ` order by seq`

// Read is the whole log in row order. It takes the pool and not a [Writer],
// because reading the log is not a reason to be handed the thing that appends
// to it.
//
// It holds every row in memory, which is what a reader of a log this package
// never deletes from should know; [Verify] does not, and is what a caller
// wanting only the answer should use.
func Read(ctx context.Context, pool *pgxpool.Pool) ([]Row, error) {
	rows, err := pool.Query(ctx, selectRows)
	if err != nil {
		return nil, fmt.Errorf("decisionlog: reading the log: %w", err)
	}
	defer rows.Close()

	var read []Row
	for rows.Next() {
		row, err := scan(rows)
		if err != nil {
			return nil, err
		}
		read = append(read, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("decisionlog: reading the log: %w", err)
	}
	return read, nil
}

func scan(rows pgx.Rows) (Row, error) {
	var row Row
	var kind, shape, part string
	err := rows.Scan(&row.Seq, &row.ID, &kind, &row.Actor.Name, &row.At, &shape,
		&row.Payload, &row.PolicyVersion, &row.ScoreVersion, &part, &row.Closes,
		&row.PrevHash, &row.Hash)
	if err != nil {
		return Row{}, fmt.Errorf("decisionlog: reading a row: %w", err)
	}
	row.Actor.Kind = record.Kind(kind)
	row.Shape = Shape(shape)
	row.Part = Part(part)
	return row, nil
}
