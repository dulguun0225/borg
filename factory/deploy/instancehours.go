package deploy

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/record"
)

// The three fleets' spans, closed one write each. Three sets of instances run
// over spans this record dates: the release's own, the control's, and the
// instances of the release a rollback of this one would return to, kept while
// any open window could return to it. Each span starts when the deploy started
// and the window opened over it, which is the record's own timestamp, and ends
// where the write below says it did.
//
// Each write records that fleet's hours, adds them to the target row's total,
// and adds what they converted to at the rate in force at that write — fixed
// there and never repriced by a rate corrected later. Where no rate was in
// force, priced.InForce is false and the amount and the rate stay null together,
// which is not an amount of zero.

// TearDownRelease closes the span of the deploy's own fleet on one target, which
// ends when the release that replaces it completes there. The deployer performs
// that replacement, so it is the deploy after this one that writes this.
func (w *Writer) TearDownRelease(ctx context.Context, id, address string, hours float64, priced Priced) error {
	return w.tearDown(ctx, id, address, "release", hours, priced)
}

// TearDownControl closes the control's span on the target it ran on. A control
// is torn down when the window it was the comparison for closes, and a deploy
// without a control never writes this.
func (w *Writer) TearDownControl(ctx context.Context, id, address string, hours float64, priced Priced) error {
	return w.tearDown(ctx, id, address, "control", hours, priced)
}

// TearDownKept closes the kept fleet's span on one target. A release's instances
// are kept at the capacity they had while any open window's rollback could
// return to that release, and torn down when the last such window closes, which
// is the step that calls this.
func (w *Writer) TearDownKept(ctx context.Context, id, address string, hours float64, priced Priced) error {
	return w.tearDown(ctx, id, address, "kept", hours, priced)
}

// tearDown is the one write the three share. The fleet's name is a constant of
// this package and never input, so writing it into the statement names no place
// anything can be injected; the three columns it composes are that fleet's date
// and hours.
func (w *Writer) tearDown(ctx context.Context, id, address, fleet string, hours float64, priced Priced) error {
	if hours < 0 {
		return fmt.Errorf("%w: %s of %s ran %v hours", ErrTargetNotFound, fleet, id, hours)
	}
	var amount, rate any
	if priced.InForce {
		amount, rate = hours*priced.Rate, priced.Rate
	}
	return w.updateTarget(ctx, id, address, "tearing down the "+fleet+" instances of",
		`update `+TargetTable+` set
			`+fleet+`_torn_down_at = $1,
			`+fleet+`_instance_hours = $2,
			instance_hours = instance_hours + $2,
			amount = case when $3::double precision is null then amount else coalesce(amount, 0) + $3 end,
			rate = case when $4::double precision is null then rate else $4 end
		where deploy_id = $5 and address = $6`, record.Now(), hours, amount, rate)
}

// Hours is the span one fleet ran, in hours, from when the deploy started to
// when that fleet was torn down, times the instances in it. It is the arithmetic
// the three writes above are given, kept here so that a caller computing it and
// a reader checking it read one function.
//
// It is a fact of fields and timestamps the record already carries, so a caller
// passes the two timestamps rather than a duration it worked out elsewhere.
func Hours(startedAt, tornDownAt string, instances int) (float64, error) {
	started, err := record.ParseTime(startedAt)
	if err != nil {
		return 0, fmt.Errorf("deploy: reading when the deploy started: %w", err)
	}
	ended, err := record.ParseTime(tornDownAt)
	if err != nil {
		return 0, fmt.Errorf("deploy: reading when the fleet was torn down: %w", err)
	}
	hours := ended.Sub(started).Hours() * float64(instances)
	if hours < 0 {
		return 0, nil
	}
	return hours, nil
}
