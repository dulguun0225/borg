package policy

import (
	"context"

	"github.com/dulguun0225/borg/factory/gatepolicy"
)

// AllowedPredicateKinds is the list in force, which is the one read package
// consumercontract's derivation performs against gate policy: the kinds a
// consumer may draw from. It is a read of its own rather than a filter over
// [Reader.All] for the reason [Reader.WindowParameters] is — All is a printer's
// answer over every parameter and this is one mechanism's over one.
//
// The subjects are the factory-wide settings record's and nothing else, which
// [safeguardsOn] adds on its own: the allowed predicate kinds are one list the
// factory owns, so a safeguard on a service or an area is a safeguard on a
// subject this parameter's mechanism never reads — the dangling safeguard the
// design already accounts for.
func (r *Reader) AllowedPredicateKinds(ctx context.Context) (Effective, error) {
	definition, err := gatepolicy.Define(gatepolicy.AllowedPredicateKinds)
	if err != nil {
		return Effective{}, err
	}
	_, authored, err := r.authored(ctx, definition, Subjects{})
	if err != nil {
		return Effective{}, err
	}
	return r.resolveList(ctx, gatepolicy.AllowedPredicateKinds, authored, Subjects{})
}
