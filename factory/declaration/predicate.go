package declaration

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrNotFound is returned where the named predicate does not exist.
	ErrNotFound = errors.New("declaration: no predicate has that id")
	// ErrIncomplete is returned for a predicate missing something every one
	// names: the consumer's item or service, the declaration version it was
	// introduced by, the producer's service, the interface, or the element.
	ErrIncomplete = errors.New("declaration: the predicate is missing something every one names")
	// ErrArgumentRefused is returned for a predicate carrying an argument its
	// kind does not take, and for one whose kind takes an argument and has none.
	// An argument nothing reads would be an assertion nobody can decide.
	ErrArgumentRefused = errors.New("declaration: the argument is not the shape this kind of predicate takes")
	// ErrArgumentUnreadable is returned for a range whose two ends are not two
	// numbers and for a domain naming nothing.
	ErrArgumentUnreadable = errors.New("declaration: the argument is not something this kind can be decided against")
)

// Predicate is one assertion a consumer's build makes about one element of one
// contract it reads, as it is stored.
//
// The producer is named twice and the two are different facts. ProducerService is
// what the consumer's own build says — a name in a file name — and is always
// there. ProducerServiceID is that name resolved to a service record at the
// moment the declaration was submitted, and is empty where it resolved to
// nothing: a consumer may declare against an interface no release has published
// yet, and a contract exists only from the merge that first published it.
type Predicate struct {
	ID    string
	Actor record.Actor
	At    string
	// ItemID and ServiceID are the consumer's: the item the declaration is an
	// artifact of, and the service that item changes.
	ItemID    string
	ServiceID string
	// ArtifactID is the declaration version this predicate was introduced by.
	// There is no field naming the release it entered force at: no release exists
	// when it is written, and the merge queue writing one later would be a second
	// writer.
	ArtifactID        string
	ProducerService   string
	ProducerServiceID string
	Interface         string
	Element           string
	Kind              gatepolicy.PredicateKind
	// Argument is the unit, the names of the domain, or the two ends of the
	// range, and is empty for a kind that takes none.
	Argument string
}

// Describe is one predicate in the words a rejection row and an owner read.
func (p Predicate) Describe() string {
	where := p.ProducerService + "." + p.Interface + "." + p.Element
	if !p.Kind.TakesAnArgument() {
		return string(p.Kind) + " " + where
	}
	return string(p.Kind) + " " + where + " = " + p.Argument
}

// Validate refuses a predicate nothing can decide: one missing a link every
// predicate has, one whose kind this factory has no decider for, and one whose
// argument is not the shape its kind takes. [Insert] calls it, and so does the
// derivation before it returns a draft.
func (p Predicate) Validate() error {
	for _, required := range []struct{ what, value string }{
		{"consumer's item", p.ItemID}, {"consumer's service", p.ServiceID},
		{"declaration version", p.ArtifactID}, {"producer's service", p.ProducerService},
		{"interface", p.Interface}, {"element", p.Element},
	} {
		if required.value == "" {
			return fmt.Errorf("%w: it names no %s", ErrIncomplete, required.what)
		}
	}
	if _, err := gatepolicy.DecidablePredicate(string(p.Kind)); err != nil {
		return err
	}
	return checkArgument(p.Kind, p.Argument)
}

// checkArgument refuses an argument of the wrong shape for the kind, and one this
// factory cannot read: a domain naming nothing, and a range that is not two
// numbers with the lower first.
func checkArgument(kind gatepolicy.PredicateKind, argument string) error {
	if !kind.TakesAnArgument() {
		if argument != "" {
			return fmt.Errorf("%w: %s asserts nothing about a value and carries %q",
				ErrArgumentRefused, kind, argument)
		}
		return nil
	}
	if argument == "" {
		return fmt.Errorf("%w: %s carries one", ErrArgumentRefused, kind)
	}
	switch kind {
	case gatepolicy.PredicateDomain:
		if len(Domain(argument)) == 0 {
			return fmt.Errorf("%w: the domain %q names nothing", ErrArgumentUnreadable, argument)
		}
	case gatepolicy.PredicateRange:
		if _, _, err := Range(argument); err != nil {
			return err
		}
	}
	return nil
}

// Domain is the names a domain predicate's argument holds, separated by a
// vertical bar. The bar rather than a comma, because the argument reaches this out
// of a struct tag whose words are comma-separated, and one separator inside
// another is a parse nobody can read back.
func Domain(argument string) []string {
	var names []string
	for _, name := range strings.Split(argument, "|") {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// Range is the two ends a range predicate's argument holds, written low..high.
func Range(argument string) (float64, float64, error) {
	low, high, found := strings.Cut(argument, "..")
	if !found {
		return 0, 0, fmt.Errorf("%w: the range %q is not written low..high", ErrArgumentUnreadable, argument)
	}
	from, err := strconv.ParseFloat(strings.TrimSpace(low), 64)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: the low end of %q is not a number", ErrArgumentUnreadable, argument)
	}
	to, err := strconv.ParseFloat(strings.TrimSpace(high), 64)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: the high end of %q is not a number", ErrArgumentUnreadable, argument)
	}
	if from > to {
		return 0, 0, fmt.Errorf("%w: the range %q has its ends the wrong way round", ErrArgumentUnreadable, argument)
	}
	return from, to, nil
}

// Result is what deciding one predicate produced. Decided is whether the thing it
// was decided against could answer at all: three of the five kinds can be decided
// against a form and all five against an observed exchange, so a domain read
// against a form is undecided rather than failed — and undecided is not a failure,
// it is the assertion waiting for the side that can answer it.
type Result struct {
	Predicate Predicate
	Decided   bool
	Held      bool
	// Why is what a reader of a rejection sees, and is empty where the predicate
	// held.
	Why string
}

// AgainstForm decides one predicate against the form a producer publishes, with
// no run to observe. It is what a consumer's own merge row checks its newly
// derived declaration with, the form being the one its producer's newest release
// publishes — which is what the consumer will meet.
func (p Predicate) AgainstForm(f contract.Form) Result {
	if !p.Kind.DecidableAgainstAForm() {
		return Result{Predicate: p}
	}
	element, found := f.Element(p.Element)
	if !found {
		return Result{Predicate: p, Decided: true,
			Why: fmt.Sprintf("%s publishes no element %s", f.Name, p.Element)}
	}
	switch p.Kind {
	case gatepolicy.PredicateRead:
		return Result{Predicate: p, Decided: true, Held: true}
	case gatepolicy.PredicatePopulated:
		if !element.Populated {
			return Result{Predicate: p, Decided: true,
				Why: fmt.Sprintf("%s.%s is not always populated", f.Name, p.Element)}
		}
		return Result{Predicate: p, Decided: true, Held: true}
	default:
		if !carriesUnit(p.Element, p.Argument) {
			return Result{Predicate: p, Decided: true,
				Why: fmt.Sprintf("the name %s does not carry the unit %s", p.Element, p.Argument)}
		}
		return Result{Predicate: p, Decided: true, Held: true}
	}
}

// carriesUnit is whether an element's name carries the unit, which is what a unit
// predicate asserts: the unit belongs to the element's identity rather than to a
// note about it, so a change of units is a rename and the name is what says which
// unit it is. The comparison is case-insensitive because the name is a field name
// and the unit is a word.
func carriesUnit(element, unit string) bool {
	return strings.HasSuffix(strings.ToLower(element), strings.ToLower(unit))
}

// Document is one observed exchange: what a producer's build wrote for one unit
// of work, as the keys the form names and the values it carried. It is a JSON
// object read generically, so a value is a string, a number, a boolean, or null,
// and a nested object or array is none of those — the form is flat here, which is
// this substrate's limit and not the design's.
type Document map[string]any

// AgainstExchange decides one predicate against every document observed on a
// candidate's own environment. All five kinds are decidable here, which is why
// this is the check a producer's merge row makes.
//
// No document at all is a failure and not a pass: a producer that emitted nothing
// has not shown that a consumer's assumption holds. A predicate is decided against
// every document rather than against one, which is stronger than the design asks —
// a kind has to be decidable against one observed exchange, and reading all of
// them is the same question asked more often.
func (p Predicate) AgainstExchange(documents []Document) Result {
	if len(documents) == 0 {
		return Result{Predicate: p, Decided: true,
			Why: "no exchange was observed, so nothing showed that this holds"}
	}
	switch p.Kind {
	case gatepolicy.PredicateRead:
		for _, d := range documents {
			if _, present := d[p.Element]; present {
				return Result{Predicate: p, Decided: true, Held: true}
			}
		}
		return Result{Predicate: p, Decided: true,
			Why: fmt.Sprintf("no observed exchange carries %s at all", p.Element)}
	case gatepolicy.PredicateUnit:
		for _, d := range documents {
			for key := range d {
				if key == p.Element {
					if carriesUnit(key, p.Argument) {
						return Result{Predicate: p, Decided: true, Held: true}
					}
					return Result{Predicate: p, Decided: true,
						Why: fmt.Sprintf("the name %s does not carry the unit %s", key, p.Argument)}
				}
			}
		}
		return Result{Predicate: p, Decided: true,
			Why: fmt.Sprintf("no observed exchange carries %s at all", p.Element)}
	}
	for at, d := range documents {
		value, present := d[p.Element]
		if !present {
			if p.Kind == gatepolicy.PredicatePopulated {
				return Result{Predicate: p, Decided: true,
					Why: fmt.Sprintf("exchange %d carries no %s", at+1, p.Element)}
			}
			// A domain and a range are about the values an element takes, so an
			// exchange that does not carry it violates neither. What catches an
			// element that stopped being there is the read or the populated
			// predicate beside them.
			continue
		}
		if why := violates(p, value); why != "" {
			return Result{Predicate: p, Decided: true,
				Why: fmt.Sprintf("exchange %d: %s", at+1, why)}
		}
	}
	return Result{Predicate: p, Decided: true, Held: true}
}

// violates is what one value does to one predicate, in words, and empty where the
// value is what the predicate asserts. A value that is neither a string nor a
// number is a violation of every kind that reads one: the form is flat here, and a
// predicate over a nested value is an assertion this substrate cannot decide.
func violates(p Predicate, value any) string {
	switch p.Kind {
	case gatepolicy.PredicatePopulated:
		switch v := value.(type) {
		case nil:
			return p.Element + " is null"
		case string:
			if strings.TrimSpace(v) == "" {
				return p.Element + " is empty"
			}
			return ""
		default:
			return ""
		}
	case gatepolicy.PredicateDomain:
		text, ok := value.(string)
		if !ok {
			return fmt.Sprintf("%s is %v, which is not one of the names a domain is written in", p.Element, value)
		}
		for _, name := range Domain(p.Argument) {
			if text == name {
				return ""
			}
		}
		return fmt.Sprintf("%s is %q, outside the domain %s", p.Element, text, p.Argument)
	case gatepolicy.PredicateRange:
		number, ok := value.(float64)
		if !ok {
			return fmt.Sprintf("%s is %v, which is not a number", p.Element, value)
		}
		from, to, err := Range(p.Argument)
		if err != nil {
			return err.Error()
		}
		if number < from || number > to {
			return fmt.Sprintf("%s is %v, outside the range %s", p.Element, number, p.Argument)
		}
		return ""
	}
	return ""
}
