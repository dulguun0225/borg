package policy

import (
	"context"

	"github.com/dulguun0225/borg/factory/gatepolicy"
)

// All is every parameter as it is in force against these subjects, in the order
// gate policy's own table lists the rows: the value, where it came from, the
// safeguards that reached it, and what reads it. It is what the command-line
// interface prints, and what says which parameters nothing reads yet rather than
// leaving an owner to discover that what they authored changed nothing.
func (r *Reader) All(ctx context.Context, s Subjects) ([]Effective, error) {
	all := make([]Effective, 0, len(gatepolicy.Definitions))
	for _, d := range gatepolicy.Definitions {
		authored, list, err := r.authored(ctx, d, s)
		if err != nil {
			return nil, err
		}
		if d.Kind == gatepolicy.KindList {
			effective, err := r.resolveList(ctx, d.Parameter, list, s)
			if err != nil {
				return nil, err
			}
			all = append(all, effective)
			continue
		}
		effective, err := r.resolve(ctx, d.Parameter, authored, s)
		if err != nil {
			return nil, err
		}
		all = append(all, effective)
	}
	return all, nil
}
