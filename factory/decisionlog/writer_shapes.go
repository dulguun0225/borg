package decisionlog

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/lease"
)

// ErrReadingMissing is returned by [Writer.AppendQueueRejection] for an entry
// naming no reading: the row always says which of the queue's readings it
// was.
var ErrReadingMissing = errors.New("decisionlog: a queue rejection names which of the queue's readings it was")

// AppendPageEvent appends a page that was delivered, which names neither
// version and closes nothing.
func (w *Writer) AppendPageEvent(ctx context.Context, e Entry) (Row, error) {
	return w.appendSimple(ctx, ShapePageEvent, e)
}

// AppendPageEventInTx appends the same row inside a transaction the caller
// opened. Its callers are components ending a wait that also write the row
// that ends it — a decision's close, an item's or an intent's own ending —
// so that a stop between the two writes never leaves the log showing a row
// still waiting with nothing to answer it, or an answered event over a row
// whose own ending never committed. It takes the fence and this package's
// advisory lock inside tx, as [withAppendTx] does for an append of its own,
// for the reason [Writer.AppendPolicyVersionInTx] does.
func (w *Writer) AppendPageEventInTx(ctx context.Context, tx pgx.Tx, e Entry) (Row, error) {
	if err := expectShape(e, ShapePageEvent); err != nil {
		return Row{}, err
	}
	if e.Closes != "" {
		return Row{}, fmt.Errorf("%w: named %q", ErrClosesRefused, e.Closes)
	}
	if err := refuseVersionsAndClosingOnlyFields(string(ShapePageEvent), e); err != nil {
		return Row{}, err
	}
	if err := e.Actor.Validate(); err != nil {
		return Row{}, err
	}
	if err := lease.Fence(ctx, tx, w.token); err != nil {
		return Row{}, err
	}
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, AdvisoryLockKey); err != nil {
		return Row{}, fmt.Errorf("decisionlog: taking the append lock: %w", err)
	}
	return insertRowTx(ctx, tx, ShapePageEvent, "", e)
}

// AppendReworkRequest appends the row written when an author sends its item
// back with no gate fired: the defect it found as [Entry.Reason], the field a
// reject's close event carries, and what owns the defect — a stage,
// decomposition, or the intent — as [Entry.ReturnsTo], the same way. Both are
// required: a rework request always names what it found and what owns it.
func (w *Writer) AppendReworkRequest(ctx context.Context, e Entry) (Row, error) {
	if err := expectShape(e, ShapeReworkRequest); err != nil {
		return Row{}, err
	}
	if e.Closes != "" {
		return Row{}, fmt.Errorf("%w: named %q", ErrClosesRefused, e.Closes)
	}
	if e.Reason == "" {
		return Row{}, fmt.Errorf("%w: a rework request", ErrReasonMissing)
	}
	if e.ReturnsTo == "" {
		return Row{}, ErrReturnsToMissing
	}
	if err := refuseVersionsOnlyFields(string(ShapeReworkRequest), e); err != nil {
		return Row{}, err
	}
	if e.Verdict != "" {
		return Row{}, fmt.Errorf("%w: a rework request named %q", ErrVerdictRefused, e.Verdict)
	}
	if e.OpenedInWorkAt != "" {
		return Row{}, fmt.Errorf("%w: a rework request named %q", ErrOpenedInWorkAtRefused, e.OpenedInWorkAt)
	}
	if e.SelfApproval {
		return Row{}, fmt.Errorf("%w: a rework request", ErrSelfApprovalRefused)
	}
	if e.Reading != "" {
		return Row{}, fmt.Errorf("%w: a rework request named %q", ErrReadingRefused, e.Reading)
	}
	if e.MovedRelease != "" {
		return Row{}, fmt.Errorf("%w: a rework request named %q", ErrMovedReleaseRefused, e.MovedRelease)
	}
	return commitAppend(ctx, w.pool, w.token, ShapeReworkRequest, "", e, nil)
}

// AppendQueueRejection appends the merge queue's rejection of a candidate:
// which of the queue's readings it was, required, and the moved release
// where a dependency's own moved, which may be empty.
func (w *Writer) AppendQueueRejection(ctx context.Context, e Entry) (Row, error) {
	if err := expectShape(e, ShapeQueueRejection); err != nil {
		return Row{}, err
	}
	if e.Closes != "" {
		return Row{}, fmt.Errorf("%w: named %q", ErrClosesRefused, e.Closes)
	}
	if e.Reading == "" {
		return Row{}, ErrReadingMissing
	}
	if err := refuseVersionsOnlyFields(string(ShapeQueueRejection), e); err != nil {
		return Row{}, err
	}
	if e.Verdict != "" {
		return Row{}, fmt.Errorf("%w: a queue rejection named %q", ErrVerdictRefused, e.Verdict)
	}
	if e.Reason != "" {
		return Row{}, fmt.Errorf("%w: a queue rejection named %q", ErrReasonRefused, e.Reason)
	}
	if e.OpenedInWorkAt != "" {
		return Row{}, fmt.Errorf("%w: a queue rejection named %q", ErrOpenedInWorkAtRefused, e.OpenedInWorkAt)
	}
	if e.SelfApproval {
		return Row{}, fmt.Errorf("%w: a queue rejection", ErrSelfApprovalRefused)
	}
	if e.ReturnsTo != "" {
		return Row{}, fmt.Errorf("%w: a queue rejection named %q", ErrReturnsToRefused, e.ReturnsTo)
	}
	return commitAppend(ctx, w.pool, w.token, ShapeQueueRejection, "", e, nil)
}

// refuseVersionsOnlyFields refuses an entry naming either version, which
// neither a rework request nor a queue rejection may.
func refuseVersionsOnlyFields(what string, e Entry) error {
	if e.PolicyVersion != "" || e.ScoreVersion != "" {
		return fmt.Errorf("%w: %s named policy %q, score %q", ErrVersionsRefused, what, e.PolicyVersion, e.ScoreVersion)
	}
	return nil
}

// AppendPolicyVersion appends the row written at each owner write and at
// each write to the People declaration other than the key-to-name mapping.
func (w *Writer) AppendPolicyVersion(ctx context.Context, e Entry) (Row, error) {
	return w.appendSimple(ctx, ShapePolicyVersion, e)
}

// AppendScoreVersion appends the row written by the score as the values it
// supplies move.
func (w *Writer) AppendScoreVersion(ctx context.Context, e Entry) (Row, error) {
	return w.appendSimple(ctx, ShapeScoreVersion, e)
}

// AppendInstallEvent appends the row written at every upgrade and at every
// start after the factory's records are restored from a backup, and the row
// the merge queue writes beside it for the numbers a mint after a restore
// passed over, which the design places beside the install event and not in a
// shape of its own.
func (w *Writer) AppendInstallEvent(ctx context.Context, e Entry) (Row, error) {
	return w.appendSimple(ctx, ShapeInstallEvent, e)
}

// appendSimple is every one-row shape that names no version, closes
// nothing, and carries none of a decision closing's own fields.
func (w *Writer) appendSimple(ctx context.Context, shape Shape, e Entry) (Row, error) {
	if err := expectShape(e, shape); err != nil {
		return Row{}, err
	}
	if e.Closes != "" {
		return Row{}, fmt.Errorf("%w: named %q", ErrClosesRefused, e.Closes)
	}
	if err := refuseVersionsAndClosingOnlyFields(string(shape), e); err != nil {
		return Row{}, err
	}
	return commitAppend(ctx, w.pool, w.token, shape, "", e, nil)
}

// AppendPolicyVersionInTx appends the same row inside a transaction the
// caller opened. Its one caller is package policy, which writes the scope
// record's field in the same transaction: the version is the trail's copy of
// what the field then holds, and a stop between the two writes is what the
// one transaction removes. It takes the fence and this package's advisory
// lock inside tx, as [withAppendTx] does for an append of its own, so a
// caller's transaction that already fenced fences again and the head is read
// under the lock.
func (w *Writer) AppendPolicyVersionInTx(ctx context.Context, tx pgx.Tx, e Entry) (Row, error) {
	if err := expectShape(e, ShapePolicyVersion); err != nil {
		return Row{}, err
	}
	if e.Closes != "" {
		return Row{}, fmt.Errorf("%w: named %q", ErrClosesRefused, e.Closes)
	}
	if err := refuseVersionsAndClosingOnlyFields(string(ShapePolicyVersion), e); err != nil {
		return Row{}, err
	}
	if err := e.Actor.Validate(); err != nil {
		return Row{}, err
	}
	if err := lease.Fence(ctx, tx, w.token); err != nil {
		return Row{}, err
	}
	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`, AdvisoryLockKey); err != nil {
		return Row{}, fmt.Errorf("decisionlog: taking the append lock: %w", err)
	}
	return insertRowTx(ctx, tx, ShapePolicyVersion, "", e)
}
