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

// Position is where in an exchange an element sits, and it is what says which
// way compatibility runs for it: an element added to what an interface returns
// breaks nobody, and a required element added to what it accepts breaks every
// caller that does not send it.
type Position string

const (
	// PositionOutput is what the interface returns.
	PositionOutput Position = "output"
	// PositionInput is what the interface accepts.
	PositionInput Position = "input"
	// PositionStore is an element of a store, which is read and written by the
	// same service and so runs both ways at once.
	PositionStore Position = "store"
)

// Positions is every position an element may be in. The CHECK constraint in
// [DDL] lists the same three.
var Positions = []Position{PositionOutput, PositionInput, PositionStore}

// ElementKind is what one element of a form is: one field, one operation, one
// argument of an operation, or one message. A consumer contract names an element
// of any of these kinds, so the diff and the predicates have to be able to state
// something about each.
type ElementKind string

const (
	// ElementField is one field of a message or one element of a store.
	ElementField ElementKind = "field"
	// ElementOperation is one operation the interface offers.
	ElementOperation ElementKind = "operation"
	// ElementArgument is one argument of one operation.
	ElementArgument ElementKind = "argument"
	// ElementMessage is one message the interface carries, whose fields are
	// elements of their own.
	ElementMessage ElementKind = "message"
)

// ElementKinds is every kind of element. The CHECK constraint in [DDL] lists
// the same four.
var ElementKinds = []ElementKind{ElementField, ElementOperation, ElementArgument, ElementMessage}

// Range is the two ends of the range an element accepts, low first. A nil
// [Element.Range] is an element that accepts any number, which is what makes a
// range appearing where there was none a narrowing.
type Range struct {
	Low  float64
	High float64
}

// Text is the range as it is stored on a predicate and printed for a human,
// written low..high.
func (r Range) Text() string { return trimNumber(r.Low) + ".." + trimNumber(r.High) }

// Element is one element of a form: what it is, where it sits, its type, the
// domain and range it accepts, whether it is required and whether it is always
// populated, the constraints a store element carries, and whether it carries a
// deprecation mark.
//
// The unit is in the name and nowhere else — SendTimeMillis and not a note
// beside SendTime — which is what makes a change of units a rename, a rename a
// breaking diff, and the four items of a migration the whole of what handles it.
// There is no field here for one, deliberately.
type Element struct {
	// Name is the element's name in the form, qualified by what it belongs to:
	// an operation is Fetch, one of its arguments is Fetch.id, a message is
	// Order, and one of its fields is Order.Status. The names are flat and
	// unique inside a form, so a consumer contract naming an element names one
	// thing.
	Name string
	Kind ElementKind
	// Position is which way compatibility runs for this element.
	Position Position
	// Type is the element's type as the build states it, compared for equality
	// and never interpreted. A change to it is a breaking diff whatever the two
	// types were, because nothing here knows which pairs of types a consumer
	// survives.
	Type string
	// Required is whether a caller has to supply the element. It is read in
	// input position, where an element added as required is breaking and one
	// added as optional is not.
	Required bool
	// Populated is whether the element is always there. An element that was
	// always populated and stops being breaks a consumer that declared it
	// populated, which is why this is part of the form and not a note about it.
	Populated bool
	// Deprecated is the mark: the old form of a pair, said by the producer's own
	// item, because no diff shows which new element supersedes which old one.
	Deprecated bool
	// Domain is the set of names the element accepts, and nil is an element that
	// accepts any. A name withdrawn from it is breaking in input position, and on
	// a store it is the domain check the store rule names.
	Domain []string
	// Range is the two numbers the element accepts, and nil is an element that
	// accepts any. Narrowing it is breaking in input position, widening it is
	// not.
	Range *Range
	// NotNull and Unique are the other two constraints the store rule names. They
	// are read on a store element and are what a declared write can violate.
	NotNull bool
	Unique  bool
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
// whose kind is neither, one with an element that has no name, no type, no kind
// or no position, one whose range has its ends the wrong way round, and one
// naming an element twice.  A derivation calls it before it returns.
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
		case !slices.Contains(ElementKinds, e.Kind):
			return fmt.Errorf("%w: element %s of %s is a %q, which is no kind of element",
				ErrFormIncomplete, e.Name, f.Name, e.Kind)
		case !slices.Contains(Positions, e.Position):
			return fmt.Errorf("%w: element %s of %s is in position %q, which is no position",
				ErrFormIncomplete, e.Name, f.Name, e.Position)
		case e.Range != nil && e.Range.Low > e.Range.High:
			return fmt.Errorf("%w: the range of %s of %s has its ends the wrong way round",
				ErrFormIncomplete, e.Name, f.Name)
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

// DomainText is a domain as it is stored on an element row and printed for a
// human: the names separated by a vertical bar. The bar rather than a comma,
// because a domain reaches a build out of a struct tag whose words are
// comma-separated.
func DomainText(names []string) string { return strings.Join(names, "|") }

// DomainNames is the names a stored domain holds, and nil for an element that
// accepts any.
func DomainNames(text string) []string {
	var names []string
	for _, name := range strings.Split(text, "|") {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// trimNumber is one end of a range in the shortest text that reads back as the
// same number, so 0..100 is not printed 0.000000..100.000000.
func trimNumber(f float64) string { return fmt.Sprintf("%g", f) }
