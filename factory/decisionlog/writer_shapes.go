package decisionlog

import (
	"context"
	"fmt"
)

// AppendPageEvent appends a page that was delivered, which names neither
// version and closes nothing.
func (w *Writer) AppendPageEvent(ctx context.Context, e Entry) (Row, error) {
	return w.appendSimple(ctx, ShapePageEvent, e)
}

// AppendReworkRequest appends the row written when an author sends its item
// back with no gate fired.
func (w *Writer) AppendReworkRequest(ctx context.Context, e Entry) (Row, error) {
	return w.appendSimple(ctx, ShapeReworkRequest, e)
}

// AppendQueueRejection appends the merge queue's rejection of a candidate
// that failed its own re-verification.
func (w *Writer) AppendQueueRejection(ctx context.Context, e Entry) (Row, error) {
	return w.appendSimple(ctx, ShapeQueueRejection, e)
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
// start after the factory's records are restored from a backup.
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
