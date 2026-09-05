package policy

import (
	"context"

	"github.com/dulguun0225/borg/factory/gatepolicy"
)

// The three parameters a gate reads beside [Reader.AtGate], each a read of one
// parameter against the firing's own subjects. They are here rather than at
// [Reader.All]'s side because a gate reads three of the thirteen and not the
// table: what it needs is a number per firing, and reading the table would
// resolve twelve values to answer for three.

// HeldOutSampleRate is how often the score auto-passes a change it would have
// gated: a field of the factory-wide settings record, the score's supplied value
// where an owner authored none, and a safeguard as a ceiling over either. The
// gate reads it after the policy has answered and hands it to the score, which
// does not know what is in force.
func (r *Reader) HeldOutSampleRate(ctx context.Context, s Subjects) (Effective, error) {
	return r.resolveOne(ctx, gatepolicy.HeldOutSampleRate, s)
}

// ReviewSampleRate is how often a change the score would have auto-passed is put
// in front of that duty's human anyway. It is one value per duty, so the
// subjects name the duty the row waits on, and a read naming none finds nothing
// authored — a value authored for one duty is not a value for another. A
// safeguard is a floor under it.
func (r *Reader) ReviewSampleRate(ctx context.Context, s Subjects) (Effective, error) {
	return r.resolveOne(ctx, gatepolicy.ReviewSampleRate, s)
}

// ExposureBound is where the exposure factor stops being weighed and puts a
// human at the row instead. It is a field of the service record, and the gate
// reads it here because the score supplies a value for the row and may not read
// what an owner authored.
func (r *Reader) ExposureBound(ctx context.Context, s Subjects) (Effective, error) {
	return r.resolveOne(ctx, gatepolicy.ExposureBound, s)
}
