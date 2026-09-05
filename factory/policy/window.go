package policy

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/gatepolicy"
)

// Window is the parameters the analysis window reads, each in force against one
// service: what an owner authored where they authored one, what the score supplies
// where they did not, and a safeguard clamping either. They are read together
// because a window resolves all of them at the open and copies them onto its record
// — a read per parameter would let an owner's write land between two of them and
// give one window a size and a confidence that were never in force at the same
// moment.
//
// Size and Power are keyed by [gatepolicy.Quantity]: both are authored per
// quantity, a detectable change in an error rate and one in a latency quantile
// not being one number. Confidence, CapSeconds and WindowLimit are one value for
// the service.
type Window struct {
	Size        map[gatepolicy.Quantity]Effective
	Power       map[gatepolicy.Quantity]Effective
	Confidence  Effective
	CapSeconds  Effective
	WindowLimit Effective
}

// WindowParameters is those for one service. It is a read of its own rather
// than a filter over [Reader.All], because All is a printer's answer over every
// subject a firing names and this is the health monitor's over one service.
func (r *Reader) WindowParameters(ctx context.Context, serviceID string) (Window, error) {
	if serviceID == "" {
		return Window{}, fmt.Errorf("policy: the analysis window's parameters are per service, and none is named")
	}
	w := Window{
		Size:  map[gatepolicy.Quantity]Effective{},
		Power: map[gatepolicy.Quantity]Effective{},
	}
	for _, q := range gatepolicy.Quantities {
		s := Subjects{ServiceID: serviceID, Quantity: string(q)}
		size, err := r.resolveOne(ctx, gatepolicy.WindowSize, s)
		if err != nil {
			return Window{}, err
		}
		w.Size[q] = size
		power, err := r.resolveOne(ctx, gatepolicy.WindowPower, s)
		if err != nil {
			return Window{}, err
		}
		w.Power[q] = power
	}

	s := Subjects{ServiceID: serviceID}
	for _, of := range []struct {
		parameter gatepolicy.Parameter
		into      *Effective
	}{
		{gatepolicy.WindowConfidence, &w.Confidence},
		{gatepolicy.WindowCap, &w.CapSeconds},
		{gatepolicy.WindowLimit, &w.WindowLimit},
	} {
		effective, err := r.resolveOne(ctx, of.parameter, s)
		if err != nil {
			return Window{}, err
		}
		*of.into = effective
	}
	return w, nil
}

// resolveOne is one parameter's authored value read and resolved against these
// subjects: [gatepolicy.Define], [Reader.authored] and [Reader.resolve] in one
// call, for a caller reading several parameters against the same subjects rather
// than through [Reader.All].
func (r *Reader) resolveOne(ctx context.Context, parameter gatepolicy.Parameter, s Subjects) (Effective, error) {
	definition, err := gatepolicy.Define(parameter)
	if err != nil {
		return Effective{}, err
	}
	authored, _, err := r.authored(ctx, definition, s)
	if err != nil {
		return Effective{}, err
	}
	return r.resolve(ctx, parameter, authored, s)
}
