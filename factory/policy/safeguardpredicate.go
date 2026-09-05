package policy

import (
	"context"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/safeguard"
)

// SafeguardPredicate is one safeguard's predicate as a mechanism reads it: the
// safeguard, who placed it, the element it is about, and the assertion. The author
// is here because a removal item blocked on a safeguard appears as an escalation
// naming the safeguard and its author, and a reader of that escalation has to know
// whom to ask.
type SafeguardPredicate struct {
	SafeguardID string
	Actor       record.Actor
	Subject     string
	Kind        gatepolicy.PredicateKind
	// Argument is the unit, the domain, or the range, and is empty for a kind
	// that takes none.
	Argument string
}

// SafeguardPredicatesOn is every safeguard's predicate in force on any of these
// contract elements, each named the way [safeguard.SubjectContractElement] names
// one. It is the read enforcement and the deprecation list both perform, and it
// is here rather than in either of them because package safeguard has one reader
// and this is it.
//
// A withdrawn safeguard is not in force and is not returned, which is what
// makes withdrawing one the way an owner takes an invented read back.
func (r *Reader) SafeguardPredicatesOn(ctx context.Context, subjects []string) ([]SafeguardPredicate, error) {
	if len(subjects) == 0 {
		return nil, nil
	}
	on := make([]safeguard.Subject, 0, len(subjects))
	for _, s := range subjects {
		if s != "" {
			on = append(on, safeguard.Subject{Kind: safeguard.SubjectContractElement, ID: s})
		}
	}
	safeguards, err := safeguard.BySubjects(ctx, r.pool, gatepolicy.SafeguardPredicate, on)
	if err != nil {
		return nil, err
	}
	predicates := make([]SafeguardPredicate, 0, len(safeguards))
	for _, p := range safeguards {
		predicates = append(predicates, SafeguardPredicate{
			SafeguardID: p.ID,
			Actor:       p.Actor,
			Subject:     p.Subject.ID,
			Kind:        p.Bound.Predicate.Kind,
			Argument:    p.Bound.Predicate.Argument,
		})
	}
	return predicates, nil
}
