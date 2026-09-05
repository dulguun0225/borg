package decisionlog

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrVersionsMissing is returned by [Writer.AppendDecisionOpen] for an
	// entry naming no policy version or no score version.
	ErrVersionsMissing = errors.New("decisionlog: an opening names a policy version and a score version")
	// ErrVersionsRefused is returned by every method but
	// [Writer.AppendDecisionOpen] and [Writer.Truncate] for an entry naming
	// either version.
	ErrVersionsRefused = errors.New("decisionlog: only a decision's opening or a truncation names a policy version or a score version")
	// ErrClosesMissing is returned by [Writer.AppendDecisionClose],
	// [Writer.AppendDecisionAbandonment], and
	// [Writer.AppendDecisionAcknowledgement] for an entry naming no row.
	ErrClosesMissing = errors.New("decisionlog: this method names the row it acts on")
	// ErrClosesRefused is returned by [Writer.AppendDecisionOpen] for an
	// entry naming a row to close.
	ErrClosesRefused = errors.New("decisionlog: an opening names no row")
	// ErrVerdictMissing is returned by [Writer.AppendDecisionClose] for an
	// entry naming no verdict or a verdict outside approve, reject, hold,
	// refer.
	ErrVerdictMissing = errors.New("decisionlog: a closing names a verdict of approve, reject, hold, or refer")
	// ErrVerdictRefused is returned by every method but
	// [Writer.AppendDecisionClose] for an entry naming a verdict.
	ErrVerdictRefused = errors.New("decisionlog: only a decision's closing names a verdict")
	// ErrReasonMissing is returned by [Writer.AppendDecisionClose] for a
	// reject or a hold with no reason, and by
	// [Writer.AppendDecisionAbandonment] for an entry with no reason.
	ErrReasonMissing = errors.New("decisionlog: a reject or a hold, and every abandonment, names a reason")
	// ErrReasonRefused is returned by every method but a decision's closing
	// and abandonment for an entry naming a reason.
	ErrReasonRefused = errors.New("decisionlog: only a decision's closing or abandonment names a reason")
	// ErrOpenedInWorkAtRefused is returned by every method but
	// [Writer.AppendDecisionClose] for an entry naming when the row was
	// opened in Work.
	ErrOpenedInWorkAtRefused = errors.New("decisionlog: only a decision's closing names when the row was opened in Work")
	// ErrOpenedInWorkAtInvalid is returned by [Writer.AppendDecisionClose]
	// for a non-empty OpenedInWorkAt that is not [record.TimeLayout].
	ErrOpenedInWorkAtInvalid = errors.New("decisionlog: when the row was opened in Work is empty or record.TimeLayout")
	// ErrSelfApprovalRefused is returned by every method but
	// [Writer.AppendDecisionClose] for an entry with SelfApproval set.
	ErrSelfApprovalRefused = errors.New("decisionlog: only a decision's closing names a self-approval")
	// ErrAcknowledgementNotHuman is returned by
	// [Writer.AppendDecisionAcknowledgement] for an actor that is not a
	// human.
	ErrAcknowledgementNotHuman = errors.New("decisionlog: only a human acknowledges a row")
)

// AppendDecisionOpen appends a decision's opening, written when the gate
// fires. It names both versions and closes nothing.
func (w *Writer) AppendDecisionOpen(ctx context.Context, e Entry) (Row, error) {
	if err := expectShape(e, ShapeDecision); err != nil {
		return Row{}, err
	}
	if e.PolicyVersion == "" || e.ScoreVersion == "" {
		return Row{}, fmt.Errorf("%w: policy %q, score %q", ErrVersionsMissing, e.PolicyVersion, e.ScoreVersion)
	}
	if e.Closes != "" {
		return Row{}, fmt.Errorf("%w: named %q", ErrClosesRefused, e.Closes)
	}
	if err := refuseClosingOnlyFields("an opening", e); err != nil {
		return Row{}, err
	}
	return commitAppend(ctx, w.pool, w.token, ShapeDecision, PartOpen, e, nil)
}

// AppendDecisionClose appends a decision's closing, written when the verdict
// is given. It names the opening it closes and neither version. It fails
// with [ErrNotAnOpening] when the named row does not exist or is not a
// decision's opening, and with [ErrAlreadyEnded] when a closing or an
// abandonment already ends it — checked here, under the advisory lock, and
// refused again by the store's unique indexes where a row reaches it around
// this method. [Writer.RefuseClose] runs after both checks and before the
// insert.
func (w *Writer) AppendDecisionClose(ctx context.Context, e Entry) (Row, error) {
	if err := expectShape(e, ShapeDecision); err != nil {
		return Row{}, err
	}
	if e.PolicyVersion != "" || e.ScoreVersion != "" {
		return Row{}, fmt.Errorf("%w: policy %q, score %q", ErrVersionsRefused, e.PolicyVersion, e.ScoreVersion)
	}
	if e.Closes == "" {
		return Row{}, fmt.Errorf("%w: a closing", ErrClosesMissing)
	}
	if e.Verdict != "approve" && e.Verdict != "reject" && e.Verdict != "hold" && e.Verdict != "refer" {
		return Row{}, fmt.Errorf("%w: got %q", ErrVerdictMissing, e.Verdict)
	}
	if (e.Verdict == "reject" || e.Verdict == "hold") && e.Reason == "" {
		return Row{}, fmt.Errorf("%w: verdict %q", ErrReasonMissing, e.Verdict)
	}
	if e.OpenedInWorkAt != "" {
		if _, err := record.ParseTime(e.OpenedInWorkAt); err != nil {
			return Row{}, fmt.Errorf("%w: %q: %v", ErrOpenedInWorkAtInvalid, e.OpenedInWorkAt, err)
		}
	}

	return commitAppend(ctx, w.pool, w.token, ShapeDecision, PartClose, e,
		func(ctx context.Context, tx pgx.Tx) error {
			if err := requireDecisionOpening(ctx, tx, e.Closes); err != nil {
				return err
			}
			ended, err := alreadyEnded(ctx, tx, e.Closes)
			if err != nil {
				return err
			}
			if ended {
				return fmt.Errorf("%w: %q", ErrAlreadyEnded, e.Closes)
			}
			if w.RefuseClose != nil {
				return w.RefuseClose(ctx, tx, e)
			}
			return nil
		})
}

// AppendDecisionAbandonment appends a decision that will never receive a
// verdict. It names the opening it ends, and [Entry.Reason] carries why no
// verdict is coming — doc.go's reuse of the closing's own reason column. It
// fails with [ErrNotAnOpening] and [ErrAlreadyEnded] the same way
// [Writer.AppendDecisionClose] does.
func (w *Writer) AppendDecisionAbandonment(ctx context.Context, e Entry) (Row, error) {
	if err := expectShape(e, ShapeDecision); err != nil {
		return Row{}, err
	}
	if e.Closes == "" {
		return Row{}, fmt.Errorf("%w: an abandonment", ErrClosesMissing)
	}
	if e.Reason == "" {
		return Row{}, fmt.Errorf("%w: an abandonment", ErrReasonMissing)
	}
	if e.Verdict != "" {
		return Row{}, fmt.Errorf("%w: an abandonment named %q", ErrVerdictRefused, e.Verdict)
	}
	if e.PolicyVersion != "" || e.ScoreVersion != "" {
		return Row{}, fmt.Errorf("%w: an abandonment", ErrVersionsRefused)
	}
	if e.OpenedInWorkAt != "" {
		return Row{}, fmt.Errorf("%w: an abandonment", ErrOpenedInWorkAtRefused)
	}
	if e.SelfApproval {
		return Row{}, fmt.Errorf("%w: an abandonment", ErrSelfApprovalRefused)
	}

	return commitAppend(ctx, w.pool, w.token, ShapeDecision, PartAbandonment, e,
		func(ctx context.Context, tx pgx.Tx) error {
			if err := requireDecisionOpening(ctx, tx, e.Closes); err != nil {
				return err
			}
			ended, err := alreadyEnded(ctx, tx, e.Closes)
			if err != nil {
				return err
			}
			if ended {
				return fmt.Errorf("%w: %q", ErrAlreadyEnded, e.Closes)
			}
			return nil
		})
}

// AppendDecisionAcknowledgement appends a human's acknowledgement of a
// decision's opening: they have the row, at Work. It fails with
// [ErrNotAnOpening] when the named row is not a decision's opening, and with
// [ErrAlreadyAcknowledged], wrapping the constraint the store refuses a
// second acknowledgement from the same human with, whether that comes
// through the method or around it.
func (w *Writer) AppendDecisionAcknowledgement(ctx context.Context, e Entry) (Row, error) {
	if err := expectShape(e, ShapeDecision); err != nil {
		return Row{}, err
	}
	if e.Closes == "" {
		return Row{}, fmt.Errorf("%w: an acknowledgement", ErrClosesMissing)
	}
	if e.Actor.Kind != record.KindHuman {
		return Row{}, fmt.Errorf("%w: actor kind %q", ErrAcknowledgementNotHuman, e.Actor.Kind)
	}
	if err := refuseClosingOnlyFields("an acknowledgement", e); err != nil {
		return Row{}, err
	}

	return commitAppend(ctx, w.pool, w.token, ShapeDecision, PartAcknowledgement, e,
		func(ctx context.Context, tx pgx.Tx) error {
			return requireDecisionOpening(ctx, tx, e.Closes)
		})
}

// requireDecisionOpening refuses with [ErrNotAnOpening] unless id names a
// row that is a decision's opening.
func requireDecisionOpening(ctx context.Context, tx pgx.Tx, id string) error {
	shape, part, err := lookupRow(ctx, tx, id)
	if err != nil {
		return err
	}
	if shape != ShapeDecision || part != PartOpen {
		return fmt.Errorf("%w: %q is shape %q, part %q", ErrNotAnOpening, id, shape, part)
	}
	return nil
}

// refuseClosingOnlyFields refuses an entry naming any of the fields only a
// decision's closing may carry: verdict, reason, when it was opened in Work,
// self-approval.
func refuseClosingOnlyFields(what string, e Entry) error {
	if e.Verdict != "" {
		return fmt.Errorf("%w: %s named %q", ErrVerdictRefused, what, e.Verdict)
	}
	if e.Reason != "" {
		return fmt.Errorf("%w: %s named %q", ErrReasonRefused, what, e.Reason)
	}
	if e.OpenedInWorkAt != "" {
		return fmt.Errorf("%w: %s named %q", ErrOpenedInWorkAtRefused, what, e.OpenedInWorkAt)
	}
	if e.SelfApproval {
		return fmt.Errorf("%w: %s", ErrSelfApprovalRefused, what)
	}
	return nil
}
