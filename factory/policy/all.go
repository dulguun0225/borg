package policy

import (
	"context"

	"github.com/dulguun0225/borg/factory/gatepolicy"
)

// All is every parameter as it is in force against these subjects, in the order
// gate policy's own table lists the rows. It is what the crude interface prints,
// and it is the one place an owner can see which of the thirteen are read by
// nothing yet.
func (r *Reader) All(ctx context.Context, s Subjects) ([]Effective, error) {
	var all []Effective
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
