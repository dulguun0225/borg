package gatepolicy

import (
	"errors"
	"fmt"
	"slices"
)

// PredicateKind is what a consumer's declaration asserts about one element of a
// contract it reads. The five here are the ones this factory can decide, and they
// are the sentences [PredicateCatalog]'s own definition lists, with "the field is
// read at all" and "it arrives populated" told apart — one is derived from the
// consumer reading the element and the other from what the consumer says about it,
// so a declaration can hold either without the other.
//
// They are vocabulary and not a table, which is why they are here: package
// declaration derives one, package policy resolves the catalog they are the
// unauthored value of, and neither should hold a second copy of the list.
type PredicateKind string

const (
	// PredicateRead is that the element is read at all. It carries no argument
	// and is the one kind derived from the consumer's code rather than from
	// anything the consumer states.
	PredicateRead PredicateKind = "read"
	// PredicatePopulated is that the element arrives populated.
	PredicatePopulated PredicateKind = "populated"
	// PredicateUnit is that the element's name carries this unit. The unit
	// belongs to the element's identity rather than to a note about it, so what
	// this asserts is about the name and not about a value.
	PredicateUnit PredicateKind = "unit"
	// PredicateDomain is that the element's values stay inside this set of
	// names.
	PredicateDomain PredicateKind = "domain"
	// PredicateRange is that the element's values stay inside these two
	// numbers.
	PredicateRange PredicateKind = "range"
)

// PredicateKinds is the catalog the factory owns: every kind a declaration may
// draw from before an owner extends it. An owner authors more on the factory
// policy record and a pin adds more still, and the value in force is the union —
// which is what makes this the floor rather than the whole catalog.
var PredicateKinds = []PredicateKind{
	PredicateRead, PredicatePopulated, PredicateUnit, PredicateDomain, PredicateRange,
}

// PredicateCatalogNames is [PredicateKinds] as the list a printer and a pin's
// union work in. The catalog is a list-valued parameter, so its value in force is
// a list of names and not a list of this type.
func PredicateCatalogNames() []string {
	names := make([]string, 0, len(PredicateKinds))
	for _, k := range PredicateKinds {
		names = append(names, string(k))
	}
	return names
}

// ErrPredicateKindUnknown is returned by [DecidablePredicate] for a kind this
// factory has no decider for. A catalog an owner widened admits the name; what
// refuses it is the derivation, which is where the design's cost of a wide
// catalog — an assertion that cannot be decided against one observed exchange —
// actually falls on this substrate.
var ErrPredicateKindUnknown = errors.New("gatepolicy: this factory has no decider for that kind of predicate")

// DecidablePredicate is the kind by that name, and an error for a name outside
// [PredicateKinds]. A caller that took the name from a catalog an owner extended
// calls this rather than casting, so a kind nothing can decide is refused where
// the declaration is derived and not where it is read.
func DecidablePredicate(name string) (PredicateKind, error) {
	kind := PredicateKind(name)
	if !slices.Contains(PredicateKinds, kind) {
		return "", fmt.Errorf("%w: %q", ErrPredicateKindUnknown, name)
	}
	return kind, nil
}

// TakesAnArgument is whether the kind's assertion needs something beside the
// element it is about: the unit, the names of the domain, the two ends of the
// range. Read and populated need none, and one carrying an argument would be an
// assertion nothing reads.
func (k PredicateKind) TakesAnArgument() bool {
	switch k {
	case PredicateUnit, PredicateDomain, PredicateRange:
		return true
	default:
		return false
	}
}

// DecidableAgainstAForm is whether the kind can be decided against a contract's
// form alone, with no run to observe. Three of the five can: whether the form has
// the element, whether the form says it is always populated, and whether its name
// carries the unit. A domain and a range are about values and need one observed
// exchange, so they are decided on the producer's side alone — which costs one
// kind of assumption being caught one gate later than the other four.
func (k PredicateKind) DecidableAgainstAForm() bool {
	switch k {
	case PredicateRead, PredicatePopulated, PredicateUnit:
		return true
	default:
		return false
	}
}
