package artifact

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
	// ErrVersionIDEmpty is returned by [Store.Redact] for a call naming no
	// version.
	ErrVersionIDEmpty = errors.New("artifact: the version id is empty")
	// ErrNotAnArtifactVersion is returned by [Store.Redact] for a redaction
	// naming a target of another kind. Each target's own writer destroys the
	// bytes inside its own records and no others.
	ErrNotAnArtifactVersion = errors.New("artifact: the redaction does not name an artifact version")
	// ErrSpanOutOfRange is returned by [Store.Redact] for a span that does
	// not fall inside the version's content.
	ErrSpanOutOfRange = errors.New("artifact: a redaction span is outside the content")
	// ErrVersionNotFound is returned by [Store.Redact] where no version has
	// that id. [Store.RedactionPass] and [Store.Replay] pass over one rather
	// than stopping: what they destroy is what there is to destroy.
	ErrVersionNotFound = errors.New("artifact: no version has that id")
	// ErrErasureRow is returned where a row of the erasure list naming an
	// artifact version cannot be read back as a version and its spans.
	ErrErasureRow = errors.New("artifact: the erasure-list row does not name a version and its spans")
)

// Span is a half-open byte range of a version's content, [Start, End), the
// unit [Store.Redact] destroys.
type Span struct {
	Start, End int
}

// Redact destroys the named spans of one version's content, in place: every
// byte inside a span is overwritten and unrecoverable, and [ContentDigest] is
// recomputed over what remains. It is the one exception to "insert and never
// update" this package otherwise holds — made for erasure and not for
// correction, which is what every other write here refuses instead of
// allowing.
//
// It appends no erasure-list row: the report store is that list's one writer,
// and the row for an artifact version is appended by the erasure action at
// Factory, through the store, before the redaction record this is handed
// exists. The spans are read against the version's own content before anything
// is written, so a span that falls outside it changes nothing.
func (s *Store) Redact(ctx context.Context, r redaction.Redaction) error {
	if r.Target.Kind != redaction.KindArtifactVersion {
		return fmt.Errorf("%w: %s", ErrNotAnArtifactVersion, r.Target)
	}
	if err := r.Actor.Validate(); err != nil {
		return err
	}
	if r.Target.ID == "" {
		return ErrVersionIDEmpty
	}
	if len(r.Spans) == 0 {
		return nil
	}
	found, err := s.destroy(ctx, r.Target.ID, spansOf(r.Spans))
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: %s", ErrVersionNotFound, r.Target.ID)
	}
	return nil
}

// RedactionPass destroys what every redaction naming an artifact version
// names, and returns how many versions it destroyed something in. It is this
// store's own pass over the redactions naming records it writes — a record
// package redaction writes and this one reads, so no component writes
// another's record. It writes no row of the erasure list either: the action
// that performed the erasure appended one through the report store before this
// pass had anything to read.
//
// A redaction naming a version this store no longer holds is passed over:
// what the pass destroys is what there is to destroy.
func (s *Store) RedactionPass(ctx context.Context) (int, error) {
	redactions, err := redaction.OverKind(ctx, s.pool, redaction.KindArtifactVersion)
	if err != nil {
		return 0, err
	}
	destroyed := 0
	for _, r := range redactions {
		if err := s.Redact(ctx, r); err != nil {
			if errors.Is(err, ErrVersionNotFound) {
				continue
			}
			return destroyed, err
		}
		destroyed++
	}
	return destroyed, nil
}

// Replay destroys again what the erasure list says was removed from an
// artifact version, and returns how many rows it destroyed something for. It
// is what this store runs against whatever a restore brought back before it
// serves again: the list is never rolled back, so a backup taken before an
// erasure carries the words and this is what takes them out again.
// erasureList is the path of the list on the host, which the composition
// holds.
func (s *Store) Replay(ctx context.Context, erasureList string) (int, error) {
	rows, err := erasurelist.ReadKind(erasureList, erasurelist.KindArtifactVersion)
	if err != nil {
		return 0, fmt.Errorf("artifact: reading the erasure list: %w", err)
	}
	destroyed := 0
	for _, row := range rows {
		versionID, spans, err := parseRemoved(row.Removed)
		if err != nil {
			return destroyed, err
		}
		found, err := s.destroy(ctx, versionID, spans)
		if err != nil {
			return destroyed, err
		}
		if found {
			destroyed++
		}
	}
	return destroyed, nil
}

// destroy overwrites the named spans of one version's content, in place,
// recomputes the digest over what remains, and reports whether the version was
// there to destroy.
func (s *Store) destroy(ctx context.Context, versionID string, spans []Span) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("artifact: beginning the redaction of %s: %w", versionID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, s.token); err != nil {
		return false, err
	}

	var content string
	err = tx.QueryRow(ctx, `select content from `+Table+` where id = $1 for update`, versionID).Scan(&content)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("artifact: reading %s to redact it: %w", versionID, err)
	}
	redacted, err := redactSpans(content, spans)
	if err != nil {
		return false, fmt.Errorf("artifact: redacting %s: %w", versionID, err)
	}
	if _, err := tx.Exec(ctx, `update `+Table+` set content = $1, content_digest = $2 where id = $3`,
		redacted, contentDigest(redacted), versionID); err != nil {
		return false, fmt.Errorf("artifact: writing the redaction of %s: %w", versionID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("artifact: committing the redaction of %s: %w", versionID, err)
	}
	return true, nil
}

// redactSpans overwrites every byte inside each span with 'x', refusing a
// span that does not fall inside content.
func redactSpans(content string, spans []Span) (string, error) {
	b := []byte(content)
	for _, sp := range spans {
		if sp.Start < 0 || sp.End > len(b) || sp.Start > sp.End {
			return "", fmt.Errorf("%w: [%d,%d) outside content of length %d", ErrSpanOutOfRange, sp.Start, sp.End, len(b))
		}
		for i := sp.Start; i < sp.End; i++ {
			b[i] = 'x'
		}
	}
	return string(b), nil
}

// spansOf is a redaction's spans as this package's own: the bounds are the
// same numbers, and the type is this package's so that a caller holding one
// cannot pass a report's spans to an artifact version.
func spansOf(spans []redaction.Span) []Span {
	mine := make([]Span, 0, len(spans))
	for _, span := range spans {
		mine = append(mine, Span{Start: span.Start, End: span.End})
	}
	return mine
}

// parseRemoved reads what an erasure-list row says was removed: the version
// the redaction named and the bounds of each span, and never a byte of what
// stood there. The report store and intake spell the same reading the same
// way over their own rows — one encoding, one name for it, and a defect in
// one found in all by one search.
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
