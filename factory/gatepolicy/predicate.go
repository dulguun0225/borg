package gatepolicy

import (
	"errors"
	"fmt"
	"slices"
)

// PredicateKind is what a consumer contract asserts about one element of a
// contract it reads or one it sends. The nine here are the ones this factory can
// decide, and they are the sentences [AllowedPredicateKinds]'s own definition
// lists, with "the field is read at all" and "it arrives populated" told apart —
// one is derived from the consumer reading the element and the other from what
// the consumer says about it, so a consumer contract can hold either without the
// other.
//
// They are vocabulary and not a table, which is why they are here: package
// consumer contract derives one, package policy resolves the list they are the
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
	// PredicateCalled is that the operation is called at all.
	PredicateCalled PredicateKind = "called"
	// PredicateSent is that this request element is sent or left out. The
	// argument is which of the two.
	PredicateSent PredicateKind = "sent"
	// PredicateSentDomain is that the values sent stay inside this set of names.
	PredicateSentDomain PredicateKind = "sent_domain"
	// PredicateSentRange is that the values sent stay inside these two numbers.
	PredicateSentRange PredicateKind = "sent_range"
)

// Side is whether a predicate is about what the consumer receives or about what
// it sends. Both are derived from the same build, and they are told apart because
// compatibility runs the other way for an input: an element a producer adds to a
// response breaks nobody, and a required element it adds to a request breaks
// every caller that does not send it.
type Side string

const (
	// SideReceived is a predicate over what the consumer receives.
	SideReceived Side = "received"
	// SideSent is a predicate over what the consumer sends.
	SideSent Side = "sent"
)

// PredicateKinds is the list of allowed predicate kinds the factory owns: every
// kind a consumer contract may draw from before an owner extends it. An owner
// authors more on the factory-wide settings record and a safeguard adds more
// still, and the value in force is the union — which is what makes this the
// floor rather than the whole list.
var PredicateKinds = []PredicateKind{
	PredicateRead, PredicatePopulated, PredicateUnit, PredicateDomain, PredicateRange,
	PredicateCalled, PredicateSent, PredicateSentDomain, PredicateSentRange,
}

// AllowedPredicateKindNames is [PredicateKinds] as the list a printer and a
// safeguard's union work in. The list of allowed predicate kinds is a
// list-valued parameter, so its value in force is a list of names and not a
// list of this type.
func AllowedPredicateKindNames() []string {
	names := make([]string, 0, len(PredicateKinds))
	for _, k := range PredicateKinds {
		names = append(names, string(k))
	}
	return names
}

// ErrPredicateKindUnknown is returned by [DecidablePredicate] for a kind this
// factory has no decider for. A list an owner widened admits the name; what
// refuses it is the derivation, which is where the design's cost of a wide
// list — an assertion that cannot be decided against one observed exchange —
// actually falls on this substrate.
var ErrPredicateKindUnknown = errors.New("gatepolicy: this factory has no decider for that kind of predicate")

// DecidablePredicate is the kind by that name, and an error for a name outside
// [PredicateKinds]. A caller that took the name from a list an owner extended
// calls this rather than casting, so a kind nothing can decide is refused where
// the consumer contract is derived and not where it is read.
func DecidablePredicate(name string) (PredicateKind, error) {
	kind := PredicateKind(name)
	if !slices.Contains(PredicateKinds, kind) {
		return "", fmt.Errorf("%w: %q", ErrPredicateKindUnknown, name)
	}
	return kind, nil
}

// Side is which side of the exchange the kind asserts about.
func (k PredicateKind) Side() Side {
	switch k {
	case PredicateCalled, PredicateSent, PredicateSentDomain, PredicateSentRange:
		return SideSent
	default:
		return SideReceived
	}
}

// TakesAnArgument is whether the kind's assertion needs something beside the
// element it is about: the unit, the names of the domain, the two ends of the
// range, and which of sent or left out is asserted. Read, populated and called
// need none, and one carrying an argument would be an assertion nothing reads.
func (k PredicateKind) TakesAnArgument() bool {
	switch k {
	case PredicateUnit, PredicateDomain, PredicateRange, PredicateSent, PredicateSentDomain, PredicateSentRange:
		return true
	default:
		return false
	}
}

// DecidableAgainstAForm is whether the kind can be decided against a contract's
// form alone, with no run to observe. Five of the nine can: whether the form has
// the element, whether the form says it is always populated, whether its name
// carries the unit, whether the form has the operation, and whether the request
// element the consumer sends or leaves out is one the form accepts or requires. A
// domain and a range are about values, on either side, and need one observed
// exchange — which costs one kind of assumption being caught one gate later than
// the rest.
func (k PredicateKind) DecidableAgainstAForm() bool {
	switch k {
	case PredicateDomain, PredicateRange, PredicateSentDomain, PredicateSentRange:
		return false
	default:
		return true
	}
}
