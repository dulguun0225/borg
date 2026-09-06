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

// InForce is one parameter as it is in force against these subjects, whichever
// of the three lists package gatepolicy defines it in. [Reader.All] answers over
// gate policy's eleven rows alone, so this is what a caller reading a parameter
// authored and not among them asks: the value in force for decision-log
// retention is a read of the factory-wide settings record and not of a row that
// list holds.
func (r *Reader) InForce(ctx context.Context, parameter gatepolicy.Parameter, s Subjects) (Effective, error) {
	d, err := gatepolicy.Define(parameter)
	if err != nil {
		return Effective{}, err
	}
	authored, list, err := r.authored(ctx, d, s)
	if err != nil {
		return Effective{}, err
	}
	if d.Kind == gatepolicy.KindList || d.Kind == gatepolicy.KindStrategy {
		return r.resolveList(ctx, parameter, list, s)
	}
	return r.resolve(ctx, parameter, authored, s)
}
