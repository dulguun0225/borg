package decisionlog

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrFormatVersionUnknown is returned when an entry's FormatVersion is
	// not in [Formats] at all: "the writer refuses a row declaring no
	// shape."
	ErrFormatVersionUnknown = errors.New("decisionlog: format version declares no known shape")
	// ErrFormatVersionMismatch is returned when an entry's FormatVersion
	// declares a shape a given method does not write.
	ErrFormatVersionMismatch = errors.New("decisionlog: format version does not declare the shape this method writes")
	// ErrNotAnOpening is returned when a closing, an abandonment, an
	// acknowledgement, or a wait's closing names a row that does not exist
	// or is not the opening the naming method expects.
	ErrNotAnOpening = errors.New("decisionlog: the row named is not the opening expected")
	// ErrAlreadyEnded is returned when a closing or an abandonment names an
	// opening a closing or an abandonment has already ended.
	ErrAlreadyEnded = errors.New("decisionlog: the row named already has a closing or an abandonment")
	// ErrAlreadyAcknowledged is returned when a second acknowledgement from
	// the same human reaches the store, whether through the method or
	// around it.
	ErrAlreadyAcknowledged = errors.New("decisionlog: this human already acknowledged the row named")
)

// Writer is the log's one writer. Every component that decides anything
// holds one and appends through it; nothing else inserts into the table.
type Writer struct {
	pool  *pgxpool.Pool
	token lease.Token
	// RefuseClose is called inside [Writer.AppendDecisionClose]'s
	// transaction, after this package's own checks pass and before the
	// insert. It is where the composition supplies the two refusals this
	// package cannot evaluate on its own: a refer with nobody left to refer
	// to, and a close whose actor authored the artifact version its opening
	// names where another holder of the row's duty exists. A nil value
	// refuses nothing extra.
	RefuseClose func(ctx context.Context, tx pgx.Tx, e Entry) error
}

// NewWriter returns the writer over pool, appending every row with token —
// the fencing token [lease.Fence] checks inside each append's own
// transaction, before the insert.
func NewWriter(pool *pgxpool.Pool, token lease.Token) *Writer {
	return &Writer{pool: pool, token: token}
}

// expectShape refuses an entry whose FormatVersion is not in [Formats]
// ([ErrFormatVersionUnknown]) or names a shape other than want
// ([ErrFormatVersionMismatch]).
func expectShape(e Entry, want Shape) error {
	got, ok := Formats[e.FormatVersion]
	if !ok {
		return fmt.Errorf("%w: %q", ErrFormatVersionUnknown, e.FormatVersion)
	}
	if got != want {
		return fmt.Errorf("%w: %q declares %s, method writes %s", ErrFormatVersionMismatch, e.FormatVersion, got, want)
	}
	return nil
}

const selectColumns = `seq, id, format_version, actor_kind, actor_key, actor_key_basis, at, shape, payload,
	policy_version, score_version, part, closes, verdict, reason, opened_in_work_at, self_approval,
	prev_hash, hash`

const selectRows = `select ` + selectColumns + ` from ` + Table + ` order by seq`

func scan(rows pgx.Rows) (Row, error) {
	var row Row
	var kind, basis, shape, part string
	err := rows.Scan(&row.Seq, &row.ID, &row.FormatVersion, &kind, &row.Actor.Key, &basis, &row.At, &shape,
		&row.Payload, &row.PolicyVersion, &row.ScoreVersion, &part, &row.Closes, &row.Verdict, &row.Reason,
		&row.OpenedInWorkAt, &row.SelfApproval, &row.PrevHash, &row.Hash)
	if err != nil {
		return Row{}, fmt.Errorf("decisionlog: reading a row: %w", err)
	}
	row.Actor.Kind = record.Kind(kind)
	row.Actor.Basis = record.Basis(basis)
	row.Shape = Shape(shape)
	row.Part = Part(part)
	return row, nil
}

// readAll is every row in row order, held in memory.
func readAll(ctx context.Context, pool *pgxpool.Pool) ([]Row, error) {
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

// lookupRow reads the shape and part of the row named id, for a method
// checking what it names before it closes or acknowledges it.
func lookupRow(ctx context.Context, tx pgx.Tx, id string) (Shape, Part, error) {
	var shape, part string
	err := tx.QueryRow(ctx, `select shape, part from `+Table+` where id = $1`, id).Scan(&shape, &part)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", fmt.Errorf("%w: %q names no row", ErrNotAnOpening, id)
	}
	if err != nil {
		return "", "", fmt.Errorf("decisionlog: reading the row named %q: %w", id, err)
	}
	return Shape(shape), Part(part), nil
}

// alreadyEnded reports whether id already has a closing or an abandonment.
func alreadyEnded(ctx context.Context, tx pgx.Tx, id string) (bool, error) {
	var ended bool
	err := tx.QueryRow(ctx,
		`select exists(select 1 from `+Table+` where closes = $1 and part in ('closing', 'abandonment'))`, id,
	).Scan(&ended)
	if err != nil {
		return false, fmt.Errorf("decisionlog: reading whether %q already ended: %w", id, err)
	}
	return ended, nil
}

const insertRow = `insert into ` + Table + `
	(seq, id, format_version, actor_kind, actor_key, actor_key_basis, at, shape, payload,
	 policy_version, score_version, part, closes, verdict, reason, opened_in_work_at, self_approval,
	 prev_hash, hash)
	values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)`

// insertRowTx reads the head, takes the next sequence value, hashes, and
// inserts, inside tx. The caller has already taken the fence and the
// advisory lock for this transaction.
func insertRowTx(ctx context.Context, tx pgx.Tx, shape Shape, part Part, e Entry) (Row, error) {
	var prevHash string
	err := tx.QueryRow(ctx, `select hash from `+Table+` order by seq desc limit 1`).Scan(&prevHash)
	if errors.Is(err, pgx.ErrNoRows) {
		prevHash = "" // The first row's predecessor hash is the empty string.
	} else if err != nil {
		return Row{}, fmt.Errorf("decisionlog: reading the head: %w", err)
	}

	// The sequence name is a constant of this package and not input, so
	// writing it into the statement is not a place anything can be
	// injected. nextval takes a regclass, which is not a type a parameter
	// carries.
	var seq int64
	if err := tx.QueryRow(ctx, `select nextval('`+Sequence+`')`).Scan(&seq); err != nil {
		return Row{}, fmt.Errorf("decisionlog: taking the next sequence value: %w", err)
	}

	row := Row{
		Seq:            seq,
		ID:             record.NewID(IDPrefix),
		FormatVersion:  e.FormatVersion,
		Actor:          e.Actor,
		At:             record.Now(),
		Shape:          shape,
		Payload:        e.Payload,
		PolicyVersion:  e.PolicyVersion,
		ScoreVersion:   e.ScoreVersion,
		Part:           part,
		Closes:         e.Closes,
		Verdict:        e.Verdict,
		Reason:         e.Reason,
		OpenedInWorkAt: e.OpenedInWorkAt,
		SelfApproval:   e.SelfApproval,
		PrevHash:       prevHash,
	}
	row.Hash = row.ChainHash()

	_, err = tx.Exec(ctx, insertRow,
		row.Seq, row.ID, row.FormatVersion, string(row.Actor.Kind), row.Actor.Key, string(row.Actor.Basis), row.At,
		string(row.Shape), row.Payload, row.PolicyVersion, row.ScoreVersion, string(row.Part), row.Closes,
		row.Verdict, row.Reason, row.OpenedInWorkAt, row.SelfApproval, row.PrevHash, row.Hash,
	)
	if err != nil {
		return Row{}, translateConstraint(fmt.Errorf("decisionlog: appending row %d: %w", row.Seq, err))
	}
	return row, nil
}

// translateConstraint wraps a unique-index violation as the domain error a
// caller that skipped this package's own pre-check would still want, so a row
// refused around the checked-for case and a row refused by going straight to
// the store around the method both come back the same way.
func translateConstraint(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.ConstraintName {
	case "decision_log_one_acknowledgement_per_human":
		return fmt.Errorf("%w: %w", ErrAlreadyAcknowledged, err)
	case "decision_log_one_closing", "decision_log_one_ending":
		return fmt.Errorf("%w: %w", ErrAlreadyEnded, err)
	default:
		return err
	}
}

// withAppendTx opens a transaction, fences it against token, takes
// [AdvisoryLockKey] for its whole duration, runs fn, and commits. Every
// append in this package goes through it, directly or through
// [commitAppend]; [Writer.Truncate] is the one caller that inserts and
// deletes inside the one fn.
func withAppendTx(ctx context.Context, pool *pgxpool.Pool, token lease.Token, fn func(ctx context.Context, tx pgx.Tx) error) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("decisionlog: beginning the append: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lease.Fence(ctx, tx, token); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, AdvisoryLockKey); err != nil {
		return fmt.Errorf("decisionlog: taking the append lock: %w", err)
	}
	if err := fn(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("decisionlog: committing: %w", err)
	}
	return nil
}

// commitAppend is every ordinary append: validate the actor, open a fenced
// and locked transaction, run checkInTx where the shape needs a row it names
// to exist and be what it expects, insert, commit. checkInTx may be nil.
func commitAppend(
	ctx context.Context, pool *pgxpool.Pool, token lease.Token, shape Shape, part Part, e Entry,
	checkInTx func(ctx context.Context, tx pgx.Tx) error,
) (Row, error) {
	if err := e.Actor.Validate(); err != nil {
		return Row{}, err
	}

	var row Row
	err := withAppendTx(ctx, pool, token, func(ctx context.Context, tx pgx.Tx) error {
		if checkInTx != nil {
			if err := checkInTx(ctx, tx); err != nil {
				return err
			}
		}
		inserted, err := insertRowTx(ctx, tx, shape, part, e)
		if err != nil {
			return err
		}
		row = inserted
		return nil
	})
	if err != nil {
		return Row{}, err
	}
	return row, nil
}
