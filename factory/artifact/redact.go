package artifact

import (
	"context"
	"errors"
	"fmt"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrVersionIDEmpty is returned by [Store.Redact] for a call naming no
	// version.
	ErrVersionIDEmpty = errors.New("artifact: the version id is empty")
	// ErrSpanOutOfRange is returned by [Store.Redact] for a span that does
	// not fall inside the version's content.
	ErrSpanOutOfRange = errors.New("artifact: a redaction span is outside the content")
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
// allowing. Its caller is this store's own redaction pass, over the
// redactions naming versions it wrote — each target's writer destroying the
// bytes it holds — and that pass is not built: the redaction record has no
// package, so there is nothing to read.
func (s *Store) Redact(ctx context.Context, actor record.Actor, versionID string, spans []Span) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if versionID == "" {
		return ErrVersionIDEmpty
	}
	if len(spans) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("artifact: beginning the redaction of %s: %w", versionID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, s.token); err != nil {
		return err
	}

	var content string
	if err := tx.QueryRow(ctx, `select content from `+Table+` where id = $1`, versionID).Scan(&content); err != nil {
		return fmt.Errorf("artifact: reading %s to redact it: %w", versionID, err)
	}
	redacted, err := redactSpans(content, spans)
	if err != nil {
		return fmt.Errorf("artifact: redacting %s: %w", versionID, err)
	}
	if _, err := tx.Exec(ctx, `update `+Table+` set content = $1, content_digest = $2 where id = $3`,
		redacted, contentDigest(redacted), versionID); err != nil {
		return fmt.Errorf("artifact: writing the redaction of %s: %w", versionID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("artifact: committing the redaction of %s: %w", versionID, err)
	}
	return nil
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
