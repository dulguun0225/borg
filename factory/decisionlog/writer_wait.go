package decisionlog

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// AppendWaitOpen appends a wait's opening, written when the factory meets a
// condition it could not compute at a firing. It closes nothing and names
// neither version.
func (w *Writer) AppendWaitOpen(ctx context.Context, e Entry) (Row, error) {
	if err := expectShape(e, ShapeWait); err != nil {
		return Row{}, err
	}
	if e.Closes != "" {
		return Row{}, fmt.Errorf("%w: %q", ErrClosesRefused, e.Closes)
	}
	if err := refuseVersionsAndClosingOnlyFields("a wait's opening", e); err != nil {
		return Row{}, err
	}
	return commitAppend(ctx, w.pool, w.token, ShapeWait, PartOpen, e, nil)
}

// AppendWaitClose appends the row written when the condition a wait's
// opening named is found gone. It names the opening it closes. It fails with
// [ErrNotAnOpening] when the named row is not a wait's opening, and with
// [ErrAlreadyEnded] when a closing already ends it.
func (w *Writer) AppendWaitClose(ctx context.Context, e Entry) (Row, error) {
	if err := expectShape(e, ShapeWait); err != nil {
		return Row{}, err
	}
	if e.Closes == "" {
		return Row{}, fmt.Errorf("%w: a wait's closing", ErrClosesMissing)
	}
	if err := refuseVersionsAndClosingOnlyFields("a wait's closing", e); err != nil {
		return Row{}, err
	}

	return commitAppend(ctx, w.pool, w.token, ShapeWait, PartClose, e,
		func(ctx context.Context, tx pgx.Tx) error {
			shape, part, err := lookupRow(ctx, tx, e.Closes)
			if err != nil {
				return err
			}
			if shape != ShapeWait || part != PartOpen {
				return fmt.Errorf("%w: %q is shape %q, part %q", ErrNotAnOpening, e.Closes, shape, part)
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

// refuseVersionsAndClosingOnlyFields refuses an entry naming either version
// or any of the fields only a decision's closing may carry — every field a
// wait's opening or closing carries none of.
func refuseVersionsAndClosingOnlyFields(what string, e Entry) error {
	if e.PolicyVersion != "" || e.ScoreVersion != "" {
		return fmt.Errorf("%w: %s named policy %q, score %q", ErrVersionsRefused, what, e.PolicyVersion, e.ScoreVersion)
	}
	return refuseClosingOnlyFields(what, e)
}
