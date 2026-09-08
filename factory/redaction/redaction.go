package redaction

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/legalhold"
	"github.com/dulguun0225/borg/factory/record"
)

// TargetKind is what a redaction names: the three records report text can be
// inside.
type TargetKind string

const (
	// KindReport is one end user's report, in the report store.
	KindReport TargetKind = "report"
	// KindStatement is the statement of the intent the reports were grouped
	// into, which intake writes.
	KindStatement TargetKind = "statement"
	// KindArtifactVersion is one version in the artifact store, which quotes
	// what it was authored against.
	KindArtifactVersion TargetKind = "artifact_version"
)

// TargetKinds is every kind a redaction may name. The CHECK in [DDL] lists
// the same three, and TestDDLListsEveryTargetKind fails if they stop
// agreeing. Each is spelled the way the erasure list spells the kind of its
// own rows, so a store reading its redactions and its erasure-list rows reads
// one word for one thing.
var TargetKinds = []TargetKind{KindReport, KindStatement, KindArtifactVersion}

var (
	// ErrTargetKindUnknown is returned for a target outside [TargetKinds].
	ErrTargetKindUnknown = errors.New("redaction: the target is not one of the three kinds")
	// ErrTargetIDEmpty is returned for a redaction naming no target.
	ErrTargetIDEmpty = errors.New("redaction: a redaction names the record it removes words from")
	// ErrReasonEmpty is returned for a redaction naming no reason.
	ErrReasonEmpty = errors.New("redaction: a redaction names its reason")
	// ErrSpansEmpty is returned for a redaction naming no span: it would
	// destroy nothing and stand as a record saying something was destroyed.
	ErrSpansEmpty = errors.New("redaction: a redaction names the spans it removes")
	// ErrSpanOutOfRange is returned for a span whose bounds are not a
	// half-open range at or after zero.
	ErrSpanOutOfRange = errors.New("redaction: a span is not a range of bytes")
	// ErrNotFound is returned where no redaction has that id or that erasure
	// key.
	ErrNotFound = errors.New("redaction: no redaction has that key")
	// ErrLegalHoldReaches is returned where a legal hold stands over the
	// target: an erasure inside a hold's reach is refused while the hold
	// stands. Recording the refusal is the caller's, this package writing
	// nothing where it refuses.
	ErrLegalHoldReaches = errors.New("redaction: a legal hold reaches this target, so the words stand")
)

// Target is what one redaction removes words from: a kind, and the id of the
// record.
type Target struct {
	Kind TargetKind
	ID   string
}

func (t Target) String() string { return string(t.Kind) + ":" + t.ID }

// Span is a half-open byte range of the target's text, [Start, End), the unit
// a redaction names and each target's own writer destroys.
type Span struct {
	Start, End int
}

// Redaction is one redaction as it is stored. It is never edited, and it
// carries no word of what it removed: the spans are bounds and the reason is
// the actor's own account of why.
type Redaction struct {
	ID     string
	Actor  record.Actor
	At     string
	Target Target
	Reason string
	Spans  []Span
	// ErasureKey is the key the erasure-list row of this erasure was appended
	// under, which [Key] derives. The row is appended before this record is
	// written, so the key cannot be this record's id.
	ErasureKey string
}

// Writing is one redaction as an owner performs it: what it is over, why, and
// which bytes go.
type Writing struct {
	Target Target
	Reason string
	Spans  []Span
}

// Key is the key the erasure-list row of one erasure is appended under: a
// digest of the actor, the target, the reason and the spans, so that the same
// erasure performed again derives the same key and appends no second row.
//
// It is not the redaction's own id, because the row lands before the record
// exists: a stop between the two steps leaves a row saying words were removed
// and no record saying they were, which is the event visibly owing rather
// than visibly done. The caller that appends the row and the write that
// stores the record both derive it here, so no second spelling of the key can
// disagree with the first.
func Key(actor record.Actor, w Writing) string {
	h := sha256.New()
	fields := []string{
		string(actor.Kind), actor.Key, string(actor.Basis),
		string(w.Target.Kind), w.Target.ID, w.Reason, encodeSpans(w.Spans),
	}
	for _, field := range fields {
		h.Write([]byte(strconv.Itoa(len(field))))
		h.Write([]byte(":"))
		h.Write([]byte(field))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Writer is the table's one writer: Factory. It wraps [Insert] with a pool
// and a token for a caller that holds no transaction of its own — package
// policy's own write calls [Insert] directly, inside the transaction that
// appends the policy version.
type Writer struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewWriter returns the writer over pool, fencing every write with token.
func NewWriter(pool *pgxpool.Pool, token lease.Token) *Writer {
	return &Writer{pool: pool, token: token}
}

// Insert writes one redaction in its own transaction, refusing it with
// [ErrLegalHoldReaches] where a hold reaches the target. See [Writer] for why
// a wrapper exists and [Reaching] for what the two halves of the refusal are.
func (w *Writer) Insert(ctx context.Context, actor record.Actor, writing Writing,
	reaches func(ctx context.Context) (bool, error)) (Redaction, error) {
	held, err := Reaching(ctx, w.pool, reaches)
	if err != nil {
		return Redaction{}, err
	}
	if held {
		return Redaction{}, fmt.Errorf("%w: %s", ErrLegalHoldReaches, writing.Target)
	}
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return Redaction{}, fmt.Errorf("redaction: beginning: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	r, err := Insert(ctx, tx, w.token, actor, writing)
	if err != nil {
		return Redaction{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Redaction{}, fmt.Errorf("redaction: committing: %w", err)
	}
	return r, nil
}

// Insert writes one redaction inside tx. Its caller is package policy's
// WriteRedaction, which appends the policy version in the same transaction,
// and which makes the legal hold's refusal before it opens one.
//
// The erasure-list row of this erasure has already been appended when this
// runs: [Key] over the same fields is what it was keyed under, and the column
// is written from that derivation rather than from an argument, so the record
// and the row cannot be keyed differently. That the row was appended at all is
// the caller's, which is the step before this one.
func Insert(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor,
	w Writing) (Redaction, error) {
	if err := lease.Fence(ctx, tx, token); err != nil {
		return Redaction{}, err
	}
	if err := actor.Validate(); err != nil {
		return Redaction{}, err
	}
	if !slices.Contains(TargetKinds, w.Target.Kind) {
		return Redaction{}, fmt.Errorf("%w: %q", ErrTargetKindUnknown, w.Target.Kind)
	}
	if w.Target.ID == "" {
		return Redaction{}, fmt.Errorf("%w: %s", ErrTargetIDEmpty, w.Target.Kind)
	}
	if w.Reason == "" {
		return Redaction{}, fmt.Errorf("%w: %s", ErrReasonEmpty, w.Target)
	}
	if len(w.Spans) == 0 {
		return Redaction{}, fmt.Errorf("%w: %s", ErrSpansEmpty, w.Target)
	}
	for _, span := range w.Spans {
		if span.Start < 0 || span.End < span.Start {
			return Redaction{}, fmt.Errorf("%w: [%d,%d)", ErrSpanOutOfRange, span.Start, span.End)
		}
	}

	r := Redaction{
		ID: record.NewID(IDPrefix), Actor: actor, At: record.Now(),
		Target: w.Target, Reason: w.Reason, Spans: w.Spans, ErasureKey: Key(actor, w),
	}
	_, err := tx.Exec(ctx, `insert into `+Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at,
		 target_kind, target_id, spans, reason, erasure_key)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		r.ID, FormatVersion, string(r.Actor.Kind), r.Actor.Key, string(r.Actor.Basis), r.At,
		string(r.Target.Kind), r.Target.ID, encodeSpans(r.Spans), r.Reason, r.ErasureKey,
	)
	if err != nil {
		return Redaction{}, fmt.Errorf("redaction: writing the redaction of %s: %w", w.Target, err)
	}
	return r, nil
}

// Reaching says whether a legal hold stands over a redaction's target, which
// is what refuses the erasure while the hold stands.
//
// Two checks make it, because a hold's subject is a service, a project, or
// the whole install and never a report, a statement or an artifact version. A
// hold on the whole install reaches every target there is, and this package
// reads that one itself through [legalhold.Reaching]. A hold on one service
// or one project reaches a target only through the records the target hangs
// from — the report's deploy, the intent's project, the version's item — none
// of which this package may import, so that half is reaches, the caller's own
// check, and a nil reaches never refuses.
func Reaching(ctx context.Context, pool *pgxpool.Pool,
	reaches func(ctx context.Context) (bool, error)) (bool, error) {
	held, err := legalhold.Reaching(ctx, pool, legalhold.Subject{Kind: legalhold.SubjectFactory})
	if err != nil {
		return false, fmt.Errorf("redaction: reading whether a legal hold stands over the install: %w", err)
	}
	if held {
		return true, nil
	}
	if reaches == nil {
		return false, nil
	}
	held, err = reaches(ctx)
	if err != nil {
		return false, fmt.Errorf("redaction: checking whether a legal hold reaches the target: %w", err)
	}
	return held, nil
}

// encodeSpans is the spans as the column holds them: each span as its start,
// a hyphen and its end, separated by spaces. It is the same encoding the
// erasure list's row carries after the target's id, so the bounds a store
// replays and the bounds it destroys from the record read the same way.
func encodeSpans(spans []Span) string {
	parts := make([]string, 0, len(spans))
	for _, span := range spans {
		parts = append(parts, strconv.Itoa(span.Start)+"-"+strconv.Itoa(span.End))
	}
	return strings.Join(parts, " ")
}

// parseSpans reads back what [encodeSpans] wrote.
func parseSpans(column string) ([]Span, error) {
	parts := strings.Fields(column)
	spans := make([]Span, 0, len(parts))
	for _, part := range parts {
		start, end, found := strings.Cut(part, "-")
		if !found {
			return nil, fmt.Errorf("%w: %q", ErrSpanOutOfRange, column)
		}
		from, err := strconv.Atoi(start)
		if err != nil {
			return nil, fmt.Errorf("%w: %q: %w", ErrSpanOutOfRange, column, err)
		}
		to, err := strconv.Atoi(end)
		if err != nil {
			return nil, fmt.Errorf("%w: %q: %w", ErrSpanOutOfRange, column, err)
		}
		spans = append(spans, Span{Start: from, End: to})
	}
	return spans, nil
}
