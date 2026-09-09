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

// TestAnOwnerExtendsTheListWithANameThatNamesADecider is C2259 "The list of
// allowed predicate kinds is one list the factory owns and an owner extends"
// held against C2229 "a name no decider exists for is refused by the
// derivation": a kind is (name, decider), and a name outside the nine shipped
// kinds is admitted where the owner's authoring pairs it with a shape one of
// the nine deciders implements, and refused where the shape itself is not one
// the factory can decide.
func TestAnOwnerExtendsTheListWithANameThatNamesADecider(t *testing.T) {
	kind, err := DecidableAuthoredKind("field_touched", PredicateRead)
	if err != nil {
		t.Fatalf("DecidableAuthoredKind: %v", err)
	}
	if kind.Name != "field_touched" || kind.Shape != PredicateRead {
		t.Errorf("DecidableAuthoredKind = %+v, want the name paired with the shape", kind)
	}

	if _, err := DecidableAuthoredKind("field_touched", "no_such_shape"); !errors.Is(err, ErrPredicateKindUnknown) {
		t.Errorf("a shape this factory has no decider for = %v, want ErrPredicateKindUnknown", err)
	}
	if _, err := DecidableAuthoredKind("", PredicateRead); !errors.Is(err, ErrPredicateKindUnknown) {
		t.Errorf("a kind with no name = %v, want ErrPredicateKindUnknown", err)
	}

	// Each of the nine shipped kinds is its own shape, so authoring one back
	// under its own name is the ordinary case and not a new one.
	for _, k := range PredicateKinds {
		got, err := DecidableAuthoredKind(string(k), k)
		if err != nil || got.Name != string(k) || got.Shape != k {
			t.Errorf("DecidableAuthoredKind(%q, %q) = %+v, %v", k, k, got, err)
		}
	}
}

// TestWhichKindsNeedARunToObserve: the received domain and the received range are
// about what a producer returns, which is not in its form, so those two need a run
// to observe. What the consumer sends is decided against what the form accepts.
func TestWhichKindsNeedARunToObserve(t *testing.T) {
	for _, k := range PredicateKinds {
		aboutValues := k == PredicateDomain || k == PredicateRange ||
			k == PredicateSentDomain || k == PredicateSentRange
		needsARun := k == PredicateDomain || k == PredicateRange
		if k.DecidableAgainstAForm() == needsARun {
			t.Errorf("%q is decidable against a form: %v", k, k.DecidableAgainstAForm())
		}
		// The unit is the third argument-taking kind on the received side, and
		// "sent or left out" is which of the two the consumer asserts.
		if k.TakesAnArgument() != (aboutValues || k == PredicateUnit || k == PredicateSent) {
			t.Errorf("%q takes an argument: %v", k, k.TakesAnArgument())
		}
	}
}
