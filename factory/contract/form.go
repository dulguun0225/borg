package contract

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Kind is what a contract is, and everything about the promise it makes follows
// from it. There are two and there is no third: a published interface promises
// backward compatibility, which is what a consumer of an interface needs, and a
// store promises forward as well, its consumer being the service's own past.
// Nothing declares which promise a contract makes — the kind decides it.
type Kind string

const (
	// KindInterface is a published interface: something other services call.
	KindInterface Kind = "interface"
	// KindStore is a service's own store. Its consumer is the release that was
	// running a minute ago, which a rollback can restore, and that is why it
	// promises forward as well as backward.
	KindStore Kind = "store"
)

// Kinds is every kind a contract may have. The CHECK constraint in [DDL] lists
// the same two, and TestDDLListsEveryKind fails if the two lists stop agreeing.
var Kinds = []Kind{KindInterface, KindStore}

// Forward reports whether the kind's promise runs in both directions. A store's
// does: the build being restored reads what the newer one wrote. Nothing declares
// this either.
func (k Kind) Forward() bool { return k == KindStore }

// Element is one element of a form: its name, its type, whether it is always
// populated, and whether it carries a deprecation mark.
//
// The unit is in the name and nowhere else — SendTimeMillis and not a note
// beside SendTime — which is what makes a change of units a rename, a rename a
// breaking diff, and the three items of a migration the whole of what handles it.
// There is no field here for one, deliberately.
type Element struct {
	Name string
	// Type is the element's type as the build states it, compared for equality
	// and never interpreted. A change to it is a breaking diff whatever the two
	// types were, because nothing here knows which pairs of types a consumer
	// survives.
	Type string
	// Populated is whether the element is always there. An element that was
	// always populated and stops being breaks a consumer that declared it
	// populated, which is why this is part of the form and not a note about it.
	Populated bool
	// Deprecated is the mark: the old form of a pair, said by the producer's own
	// item, because no diff shows which new element supersedes which old one.
	Deprecated bool
}

// Form is what one contract's build publishes at one commit: the interface's own
// name in that build, its kind, and its elements ordered by name. It is what the
// derivation produces and what [Diff] compares; it is not a record — the record
// is the version and its element rows.
type Form struct {
	Name     string
	Kind     Kind
	Elements []Element
}

// ErrFormIncomplete is returned for a form missing its name or its kind, and for
// one naming an element twice.
var ErrFormIncomplete = errors.New("contract: the form is missing something every form has")

// Validate refuses a form nothing can be diffed against: one with no name, one
// whose kind is neither, one with an element that has no name or no type, and one
// naming an element twice. A derivation calls it before it returns.
func (f Form) Validate() error {
	if f.Name == "" {
		return fmt.Errorf("%w: it has no name", ErrFormIncomplete)
	}
	if !slices.Contains(Kinds, f.Kind) {
		return fmt.Errorf("%w: %q is neither a published interface nor a store", ErrFormIncomplete, f.Kind)
	}
	seen := make(map[string]bool, len(f.Elements))
	for _, e := range f.Elements {
		switch {
		case e.Name == "":
			return fmt.Errorf("%w: %s has an element with no name", ErrFormIncomplete, f.Name)
		case e.Type == "":
			return fmt.Errorf("%w: element %s of %s has no type", ErrFormIncomplete, e.Name, f.Name)
		case seen[e.Name]:
			return fmt.Errorf("%w: %s names element %s twice", ErrFormIncomplete, f.Name, e.Name)
		}
		seen[e.Name] = true
	}
	return nil
}

// Element is the element of that name, and false where the form has none.
func (f Form) Element(name string) (Element, bool) {
	for _, e := range f.Elements {
		if e.Name == name {
			return e, true
		}
	}
	return Element{}, false
}

// Marked is the names of the elements carrying a deprecation mark, in the form's
// own order. It is what the deprecation list is asked about, one element at a
// time.
func (f Form) Marked() []string {
	var marked []string
	for _, e := range f.Elements {
		if e.Deprecated {
			marked = append(marked, e.Name)
		}
	}
	return marked
}

// Sorted is the form with its elements ordered by name, which is the order a
// derivation returns and the order [Diff] walks. Two builds that declare the same
// elements in a different order publish the same form, so the order cannot be part
// of the identity.
func (f Form) Sorted() Form {
	elements := slices.Clone(f.Elements)
	slices.SortFunc(elements, func(a, b Element) int { return strings.Compare(a.Name, b.Name) })
	f.Elements = elements
	return f
}
