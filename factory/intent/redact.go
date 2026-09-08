package intent

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/erasurelist"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/redaction"
)

var (
	// ErrNotAStatement is returned by [Intake.Redact] for a redaction naming a
	// target of another kind. Each target's own writer destroys its own bytes,
	// and the statement is the one this package holds.
	ErrNotAStatement = errors.New("intent: the redaction does not name a statement")
	// ErrSpanOutOfRange is returned where a redaction names a span that does
	// not fall inside the statement.
	ErrSpanOutOfRange = errors.New("intent: a redaction span is outside the statement")
	// ErrErasureRow is returned where a row of the erasure list naming a
	// statement cannot be read back as an intent and its spans.
	ErrErasureRow = errors.New("intent: the erasure-list row does not name a statement and its spans")
)

// Redact destroys the spans one redaction names inside the statement it names.
// It appends no erasure-list row: the report store is that list's one writer,
// and the row for a statement is appended by the erasure action at Factory,
// through the store, before the redaction record this is handed exists. So
// this is the destruction alone, and it is not what makes the erasure
// effective — a read of the statement is served through the redactions naming
// it whether or not this has run.
//
// The spans are read against the statement's own length before anything is
// written, so a span that falls outside the words changes nothing.
//
// The bytes are overwritten in place and the statement's length is unchanged,
// so a redaction applied twice destroys the same range twice and the second
// time changes nothing.
func (i *Intake) Redact(ctx context.Context, r redaction.Redaction) error {
	if r.Target.Kind != redaction.KindStatement {
		return fmt.Errorf("%w: %s", ErrNotAStatement, r.Target)
	}
	if r.Target.ID == "" {
		return ErrIntentIDEmpty
	}
	if len(r.Spans) == 0 {
		return nil
	}
	found, err := i.destroy(ctx, r.Target.ID, r.Spans)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: %s", ErrIntentNotFound, r.Target.ID)
	}
	return nil
}

// RedactionPass destroys what every redaction naming a statement names, and
// returns how many it destroyed something for. It is intake's own pass over
// the redactions naming records it writes — a record one component writes and
// another reads, so no component writes another's record. It writes no row of
// the erasure list either: the action that performed the erasure appended one
// through the report store before this pass had anything to read.
//
// A redaction naming an intent no longer here is passed over: what the pass
// destroys is what there is to destroy.
func (i *Intake) RedactionPass(ctx context.Context) (int, error) {
	redactions, err := redaction.OverKind(ctx, i.pool, redaction.KindStatement)
	if err != nil {
		return 0, err
	}
	destroyed := 0
	for _, r := range redactions {
		if err := i.Redact(ctx, r); err != nil {
			if errors.Is(err, ErrIntentNotFound) {
				continue
			}
			return destroyed, err
		}
		destroyed++
	}
	return destroyed, nil
}

// Replay destroys again what the erasure list says was removed from a
// statement, and returns how many rows it destroyed something for. It is what
// this store runs against whatever a restore brought back before it serves
// again: the list is never rolled back, so a backup taken before an erasure
// carries the words and this is what takes them out again. erasureList is the
// path of the list on the host, which the composition holds.
//
// A row naming an intent this store no longer holds is passed over.
func (i *Intake) Replay(ctx context.Context, erasureList string) (int, error) {
	rows, err := erasurelist.ReadKind(erasureList, erasurelist.KindStatement)
	if err != nil {
		return 0, fmt.Errorf("intent: reading the erasure list: %w", err)
	}
	destroyed := 0
	for _, row := range rows {
		intentID, spans, err := parseRemoved(row.Removed)
		if err != nil {
			return destroyed, err
		}
		found, err := i.destroy(ctx, intentID, spans)
		if err != nil {
			return destroyed, err
		}
		if found {
			destroyed++
		}
	}
	return destroyed, nil
}

// destroy overwrites the named spans of one intent's statement, in place, and
// reports whether the intent was there to destroy. It is the one write in this
// package that changes a statement: the statement is written once at the
// arrival and never updated, and this is erasure rather than correction. It
// makes none of the checks the other writes make — a delivered intent's words
// are erased the same as an unrefined one's.
func (i *Intake) destroy(ctx context.Context, intentID string, spans []redaction.Span) (bool, error) {
	tx, err := i.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("intent: beginning the destruction of %s: %w", intentID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, i.token); err != nil {
		return false, err
	}

	var statement string
	err = tx.QueryRow(ctx, `select statement from `+Table+` where id = $1 for update`, intentID).Scan(&statement)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("intent: reading %s to destroy what a redaction names: %w", intentID, err)
	}
	destroyed, err := destroySpans(statement, spans)
	if err != nil {
		return false, fmt.Errorf("intent: destroying spans of %s: %w", intentID, err)
	}
	if _, err := tx.Exec(ctx, `update `+Table+` set statement = $1 where id = $2`,
		destroyed, intentID); err != nil {
		return false, fmt.Errorf("intent: writing the destruction of %s: %w", intentID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("intent: committing the destruction of %s: %w", intentID, err)
	}
	return true, nil
}

// destroySpans overwrites every byte inside each span with 'x', refusing a
// span that does not fall inside text. The length is unchanged, so what a
// reader meets is a stated gap where words were, and the row, its links and
// every count over it stand.
func destroySpans(text string, spans []redaction.Span) (string, error) {
	b := []byte(text)
	for _, span := range spans {
		if span.Start < 0 || span.End > len(b) || span.Start > span.End {
			return "", fmt.Errorf("%w: [%d,%d) outside a statement of length %d",
				ErrSpanOutOfRange, span.Start, span.End, len(b))
		}
		for i := span.Start; i < span.End; i++ {
			b[i] = 'x'
		}
	}
	return string(b), nil
}

// parseRemoved reads what an erasure-list row says was removed: the intent the
// redaction named and the bounds of each span, and never a byte of what stood
// there. The report store spells the same reading the same way over its own
// rows — one encoding, one name for it, and a defect in one found in both by
// one search.
func parseRemoved(row string) (string, []redaction.Span, error) {
	parts := strings.Fields(row)
	if len(parts) < 2 {
		return "", nil, fmt.Errorf("%w: %q", ErrErasureRow, row)
	}
	spans := make([]redaction.Span, 0, len(parts)-1)
	for _, part := range parts[1:] {
		start, end, found := strings.Cut(part, "-")
		if !found {
			return "", nil, fmt.Errorf("%w: %q", ErrErasureRow, row)
		}
		from, err := strconv.Atoi(start)
		if err != nil {
			return "", nil, fmt.Errorf("%w: %q: %w", ErrErasureRow, row, err)
		}
		to, err := strconv.Atoi(end)
		if err != nil {
			return "", nil, fmt.Errorf("%w: %q: %w", ErrErasureRow, row, err)
		}
		spans = append(spans, redaction.Span{Start: from, End: to})
	}
	return parts[0], spans, nil
}
