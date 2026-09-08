package reportstore

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/erasurelist"
)

var (
	// ErrSpanOutOfRange is returned where a redaction names a span that does
	// not fall inside the report's text.
	ErrSpanOutOfRange = errors.New("reportstore: a redaction span is outside the text")
	// ErrRedactionIDEmpty is returned by [Store.Redact] for a redaction with
	// no id. The id is the key its erasure-list row is written under, so a
	// redaction without one is a row that could be appended twice.
	ErrRedactionIDEmpty = errors.New("reportstore: the redaction id is empty")
	// ErrErasureRow is returned where a row of the erasure list naming a
	// report cannot be read back as a report and its spans.
	ErrErasureRow = errors.New("reportstore: the erasure-list row does not name a report and its spans")
)

// Redact destroys the spans one redaction names inside the report it names,
// after appending the matching erasure-list row. The row lands first, keyed
// by the redaction's id so the same redaction applied again appends nothing,
// because it is what says the words must not come back with a restore.
//
// The bytes are overwritten in place and the report's length is unchanged, so
// a redaction applied twice destroys the same range twice and the second time
// changes nothing.
func (s *Store) Redact(ctx context.Context, redaction Redaction) error {
	if redaction.ID == "" {
		return ErrRedactionIDEmpty
	}
	if redaction.ReportID == "" {
		return ErrIDEmpty
	}
	if len(redaction.Spans) == 0 {
		return nil
	}
	if err := erasurelist.Append(s.erasureList, redaction.ID, erasurelist.KindReport,
		removed(redaction)); err != nil {
		return fmt.Errorf("reportstore: appending the erasure-list row for %s: %w", redaction.ID, err)
	}

	found, err := s.destroy(ctx, redaction.ReportID, redaction.Spans)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: %s", ErrNotFound, redaction.ReportID)
	}
	return nil
}

// RedactionPass destroys what every redaction naming a report names, and
// returns how many it destroyed something for. It is this store's own pass:
// the redaction is a record of the factory's graph and this store is a second
// database, so the composition hands the redactions across rather than the
// writer of one reaching in here.
//
// A read is already served through the redactions naming the report it
// returns, so what this pass adds is the destruction and never the effect.
func (s *Store) RedactionPass(ctx context.Context) (int, error) {
	redactions, err := s.redactions.OverReports(ctx)
	if err != nil {
		return 0, fmt.Errorf("reportstore: reading the redactions naming a report: %w", err)
	}
	destroyed := 0
	for _, redaction := range redactions {
		if err := s.Redact(ctx, redaction); err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return destroyed, err
		}
		destroyed++
	}
	return destroyed, nil
}

// Replay destroys again what the erasure list says was removed from a report,
// and returns how many rows it destroyed something for. It is what a store
// runs against whatever a restore brought back before it serves again: the
// list is never rolled back, so a backup taken before an erasure carries the
// words and this is what takes them out again.
//
// A row naming a report this store no longer holds is passed over: what the
// restore brought back is what there is to destroy.
func (s *Store) Replay(ctx context.Context) (int, error) {
	rows, err := erasurelist.ReadKind(s.erasureList, erasurelist.KindReport)
	if err != nil {
		return 0, fmt.Errorf("reportstore: reading the erasure list: %w", err)
	}
	destroyed := 0
	for _, row := range rows {
		reportID, spans, err := parseRemoved(row.Removed)
		if err != nil {
			return destroyed, err
		}
		found, err := s.destroy(ctx, reportID, spans)
		if err != nil {
			return destroyed, err
		}
		if found {
			destroyed++
		}
	}
	return destroyed, nil
}

// destroy overwrites the named spans of one report's text, in place, and
// reports whether the report was there to destroy. It is the one write in
// this package that changes a report's words: everything else here inserts or
// marks.
func (s *Store) destroy(ctx context.Context, reportID string, spans []Span) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("reportstore: beginning the destruction of %s: %w", reportID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var text string
	err = tx.QueryRow(ctx, `select text from `+ReportTable+` where id = $1`, reportID).Scan(&text)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("reportstore: reading %s to destroy what a redaction names: %w", reportID, err)
	}
	destroyed, err := destroySpans(text, spans)
	if err != nil {
		return false, fmt.Errorf("reportstore: destroying spans of %s: %w", reportID, err)
	}
	if _, err := tx.Exec(ctx, `update `+ReportTable+` set text = $1 where id = $2`,
		destroyed, reportID); err != nil {
		return false, fmt.Errorf("reportstore: writing the destruction of %s: %w", reportID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("reportstore: committing the destruction of %s: %w", reportID, err)
	}
	return true, nil
}

// destroySpans overwrites every byte inside each span with 'x', refusing a
// span that does not fall inside text. The length is unchanged, so what a
// reader meets is a stated gap where words were.
func destroySpans(text string, spans []Span) (string, error) {
	b := []byte(text)
	for _, span := range spans {
		if span.Start < 0 || span.End > len(b) || span.Start > span.End {
			return "", fmt.Errorf("%w: [%d,%d) outside text of length %d",
				ErrSpanOutOfRange, span.Start, span.End, len(b))
		}
		for i := span.Start; i < span.End; i++ {
			b[i] = 'x'
		}
	}
	return string(b), nil
}

// removed is what the erasure-list row says was removed: the report the
// redaction named and the bounds of each span, and never a byte of what stood
// there. [parseRemoved] reads it back, which is what makes a replay possible
// from the list alone.
func removed(redaction Redaction) string {
	parts := make([]string, 0, len(redaction.Spans)+1)
	parts = append(parts, redaction.ReportID)
	for _, span := range redaction.Spans {
		parts = append(parts, strconv.Itoa(span.Start)+"-"+strconv.Itoa(span.End))
	}
	return strings.Join(parts, " ")
}

// parseRemoved reads a row [removed] wrote back as the report and the spans.
func parseRemoved(row string) (string, []Span, error) {
	parts := strings.Fields(row)
	if len(parts) < 2 {
		return "", nil, fmt.Errorf("%w: %q", ErrErasureRow, row)
	}
	spans := make([]Span, 0, len(parts)-1)
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
		spans = append(spans, Span{Start: from, End: to})
	}
	return parts[0], spans, nil
}
