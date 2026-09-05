package policy

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/gatepolicy"
)

// Window is the four parameters the analysis window reads, each in force against one
// service: what an owner authored where they authored one, what the score supplies
// where they did not, and a safeguard clamping either. The four are read together
// because a window resolves all of them at the open and copies them onto its record
// — a read per parameter would let an owner's write land between two of them and
// give one window a size and a confidence that were never in force at the same
// moment.
type Window struct {
	Size        Effective
	Confidence  Effective
	CapSeconds  Effective
	WindowLimit Effective
}

// WindowParameters is those four for one service. It is a read of its own rather
// than a filter over [Reader.All], because All is a printer's answer over every
// subject a firing names and this is the health monitor's over one service.
func (r *Reader) WindowParameters(ctx context.Context, serviceID string) (Window, error) {
	if serviceID == "" {
		return Window{}, fmt.Errorf("policy: the analysis window's parameters are per service, and none is named")
	}
	s := Subjects{ServiceID: serviceID}
	var w Window
	for _, of := range []struct {
		parameter gatepolicy.Parameter
		into      *Effective
	}{
		{gatepolicy.WindowSize, &w.Size},
		{gatepolicy.WindowConfidence, &w.Confidence},
		{gatepolicy.WindowCap, &w.CapSeconds},
		{gatepolicy.WindowLimit, &w.WindowLimit},
	} {
		definition, err := gatepolicy.Define(of.parameter)
		if err != nil {
			return Window{}, err
		}
		authored, _, err := r.authored(ctx, definition, s)
		if err != nil {
			return Window{}, err
		}
		effective, err := r.resolve(ctx, of.parameter, authored, s)
		if err != nil {
			return Window{}, err
		}
		*of.into = effective
	}
	return w, nil
}
