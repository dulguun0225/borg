package gatepolicy

import (
	"errors"
	"slices"
	"testing"
)

// TestBothSidesOfWhatAConsumerDeclares: the list holds what the consumer receives
// and what it sends, because compatibility runs the other way for an input — an
// element a producer adds to a response breaks nobody, and a required element it
// adds to a request breaks every caller that does not send it.
func TestBothSidesOfWhatAConsumerDeclares(t *testing.T) {
	read := []PredicateKind{PredicateRead, PredicatePopulated, PredicateUnit, PredicateDomain, PredicateRange}
	sent := []PredicateKind{PredicateCalled, PredicateSent, PredicateSentDomain, PredicateSentRange}
	if len(PredicateKinds) != len(read)+len(sent) {
		t.Fatalf("the allowed kinds are %v, want the five over what a consumer receives and the four over what it sends", PredicateKinds)
	}
	for _, k := range read {
		if !slices.Contains(PredicateKinds, k) || k.Side() != SideReceived {
			t.Errorf("%q is on the %q side and %v", k, k.Side(), slices.Contains(PredicateKinds, k))
		}
	}
	for _, k := range sent {
		if !slices.Contains(PredicateKinds, k) || k.Side() != SideSent {
			t.Errorf("%q is on the %q side and %v", k, k.Side(), slices.Contains(PredicateKinds, k))
		}
	}
	if !slices.Equal(AllowedPredicateKindNames()[:1], []string{string(PredicateRead)}) {
		t.Errorf("the names are %v, want the kinds in the order the list declares them", AllowedPredicateKindNames())
	}
	if _, err := DecidablePredicate("shape"); !errors.Is(err, ErrPredicateKindUnknown) {
		t.Errorf("a kind this factory has no decider for = %v, want ErrPredicateKindUnknown", err)
	}
}

// TestWhichKindsNeedARunToObserve: a domain and a range are about values, on
// either side, and everything else is decidable against a form alone.
func TestWhichKindsNeedARunToObserve(t *testing.T) {
	for _, k := range PredicateKinds {
		aboutValues := k == PredicateDomain || k == PredicateRange ||
			k == PredicateSentDomain || k == PredicateSentRange
		if k.DecidableAgainstAForm() == aboutValues {
			t.Errorf("%q is decidable against a form: %v", k, k.DecidableAgainstAForm())
		}
		// The unit is the third argument-taking kind on the received side, and
		// "sent or left out" is which of the two the consumer asserts.
		if k.TakesAnArgument() != (aboutValues || k == PredicateUnit || k == PredicateSent) {
			t.Errorf("%q takes an argument: %v", k, k.TakesAnArgument())
		}
	}
}
