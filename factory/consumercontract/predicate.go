package consumercontract

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
	ErrNotFound = errors.New("consumercontract: no predicate has that id")
	// ErrIncomplete is returned for a predicate missing something every one
	// names: the consumer's item or service, the consumer contract version it was
	// introduced by, the producer's service, the interface, the element, or the
	// address entry the call site read its address from.
	ErrIncomplete = errors.New("consumercontract: the predicate is missing something every one names")
	// ErrArgumentRefused is returned for a predicate carrying an argument its
	// kind does not take, and for one whose kind takes an argument and has none.
	// An argument nothing reads would be an assertion nobody can decide.
	ErrArgumentRefused = errors.New("consumercontract: the argument is not the shape this kind of predicate takes")
	// ErrArgumentUnreadable is returned for a range whose two ends are not two
	// numbers, for a domain naming nothing, and for a "sent or left out" that is
	// neither.
	ErrArgumentUnreadable = errors.New("consumercontract: the argument is not something this kind can be decided against")
)

// The two values a [gatepolicy.PredicateSent] predicate's argument takes: which
// of sending the element and leaving it out the consumer asserts.
const (
	Sent    = "sent"
	LeftOut = "left out"
)

// Predicate is one assertion a consumer's build makes about one element of one
// contract it reads or one it sends to, as it is stored.
//
// The producer is named three times and the three are different facts. Address is
// the entry of the consumer's configuration file the call site reads its address
// from, which is the only place the edge between two services is held.
// ProducerService is the service that entry names, and is always there.
// ProducerServiceID is that name resolved to a service record at the moment the
// consumer contract was submitted, and is empty where it resolved to nothing: a
// consumer may declare against an interface no release has published yet, and a
// contract exists only from the merge that first published it.
type Predicate struct {
	ID    string
	Actor record.Actor
	At    string
	// ItemID and ServiceID are the consumer's: the item the consumer contract is
	// an artifact of, and the service that item changes.
	ItemID    string
	ServiceID string
	// ArtifactID is the consumer contract version this predicate was introduced
	// by. There is no field naming the release it entered force at: no release
	// exists when it is written, and the merge queue writing one later would be a
	// second writer.
	ArtifactID        string
	Address           string
	ProducerService   string
	ProducerServiceID string
	Interface         string
	Element           string
	Kind              gatepolicy.PredicateKind
	// Argument is the unit, the names of the domain, the two ends of the range,
	// or which of sent and left out is asserted, and is empty for a kind that
	// takes none.
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
		{"consumer contract version", p.ArtifactID}, {"address entry", p.Address},
		{"producer's service", p.ProducerService},
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
// factory cannot read: a domain naming nothing, a range that is not two numbers
// with the lower first, and a "sent or left out" that is neither.
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
	case gatepolicy.PredicateDomain, gatepolicy.PredicateSentDomain:
		if len(Domain(argument)) == 0 {
			return fmt.Errorf("%w: the domain %q names nothing", ErrArgumentUnreadable, argument)
		}
	case gatepolicy.PredicateRange, gatepolicy.PredicateSentRange:
		if _, _, err := Range(argument); err != nil {
			return err
		}
	case gatepolicy.PredicateSent:
		if argument != Sent && argument != LeftOut {
			return fmt.Errorf("%w: %q is neither %q nor %q", ErrArgumentUnreadable, argument, Sent, LeftOut)
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
// was decided against could answer at all, and undecided is the third outcome a
// predicate has for the reason a criterion does: a predicate decided against
// nothing passes every check the factory has and assures nothing, so where the
// candidate's run produced no exchange to decide it against it is undecided rather
// than passing. Undecided is read at the Merge to master gate the way a failure
// is, which is the caller's reading and not this package's.
type Result struct {
	Predicate Predicate
	Decided   bool
	Held      bool
	// Why is what a reader of a rejection sees. It is empty where the predicate
	// held and names what could not answer where it is undecided.
	Why string
}

// AgainstForm decides one predicate against the form a producer publishes, with
// no run to observe. It is what a consumer's own merge row checks its newly
// derived consumer contract with, the form being the one its producer's newest
// release publishes — which is what the consumer will meet — and what a producer's
// merge row decides a send-side predicate with, the form being its own request
// form.
func (p Predicate) AgainstForm(f contract.Form) Result {
	if !p.Kind.DecidableAgainstAForm() {
		return Result{Predicate: p, Why: "the form does not say what values a producer returns"}
	}
	element, found := f.Element(p.Element)
	if !found {
		if p.Kind == gatepolicy.PredicateSent && p.Argument == LeftOut {
			// An element the consumer leaves out and the form no longer has is
			// not a break: what it sends still fits.
			return Result{Predicate: p, Decided: true, Held: true}
		}
		return Result{Predicate: p, Decided: true,
			Why: fmt.Sprintf("%s publishes no element %s", f.Name, p.Element)}
	}
	switch p.Kind {
	case gatepolicy.PredicateRead, gatepolicy.PredicateCalled:
		return Result{Predicate: p, Decided: true, Held: true}
	case gatepolicy.PredicatePopulated:
		if !element.Populated {
			return Result{Predicate: p, Decided: true,
				Why: fmt.Sprintf("%s.%s is not always populated", f.Name, p.Element)}
		}
	case gatepolicy.PredicateUnit:
		if !carriesUnit(p.Element, p.Argument) {
			return Result{Predicate: p, Decided: true,
				Why: fmt.Sprintf("the name %s does not carry the unit %s", p.Element, p.Argument)}
		}
	case gatepolicy.PredicateSent:
		if p.Argument == LeftOut && element.Required {
			return Result{Predicate: p, Decided: true,
				Why: fmt.Sprintf("%s.%s is required and this consumer leaves it out", f.Name, p.Element)}
		}
	case gatepolicy.PredicateSentDomain:
		for _, name := range Domain(p.Argument) {
			if len(element.Domain) > 0 && !contains(element.Domain, name) {
				return Result{Predicate: p, Decided: true,
					Why: fmt.Sprintf("this consumer sends %s=%q and %s accepts %s",
						p.Element, name, f.Name, contract.DomainText(element.Domain))}
			}
		}
	case gatepolicy.PredicateSentRange:
		from, to, err := Range(p.Argument)
		if err != nil {
			return Result{Predicate: p, Decided: true, Why: err.Error()}
		}
		if element.Range != nil && (from < element.Range.Low || to > element.Range.High) {
			return Result{Predicate: p, Decided: true,
				Why: fmt.Sprintf("this consumer sends %s inside %s and %s accepts %s",
					p.Element, p.Argument, f.Name, element.Range.Text())}
		}
	}
	return Result{Predicate: p, Decided: true, Held: true}
}

// carriesUnit is whether an element's name carries the unit, which is what a unit
// predicate asserts: the unit belongs to the element's identity rather than to a
// note about it, so a change of units is a rename and the name is what says which
// unit it is. The comparison is case-insensitive because the name is a field name
// and the unit is a word.
func carriesUnit(element, unit string) bool {
	return strings.HasSuffix(strings.ToLower(element), strings.ToLower(unit))
}

func contains(names []string, name string) bool {
	for _, one := range names {
		if one == name {
			return true
		}
	}
	return false
}

// Document is one observed exchange, or one row of the store a candidate's run
// wrote into: what the build wrote for one unit of work, as the keys the form
// names and the values it carried. It is a JSON object read generically, so a
// value is a string, a number, a boolean, or null, and a nested object or array is
// none of those — the form is flat here, which is this platform's limit and not
// the design's.
type Document map[string]any

// AgainstExchange decides one predicate against every document observed on a
// candidate's own environment, which for a store is the state its own environment
// holds after the run.
//
// No document at all is undecided and not a failure and not a pass: a producer
// that emitted nothing has shown nothing either way, and the consumer whose path a
// run never exercised is exactly the consumer the check exists for. So is a domain
// or a range whose element appeared in no document. A predicate decided against
// nothing passes every check the factory has and assures nothing, and undecided is
// read at the gate the way a failure is.
//
// A predicate is decided against every document rather than against one, which is
// stronger than the design asks — a kind has to be decidable against one observed
// exchange, and reading all of them is the same question asked more often.
func (p Predicate) AgainstExchange(documents []Document) Result {
	if len(documents) == 0 {
		return Result{Predicate: p, Why: "the run produced no exchange to decide this against"}
	}
	if p.Kind == gatepolicy.PredicateCalled {
		// Whether the consumer calls the operation is a fact about the consumer's
		// build, not about what the producer's run wrote.
		return Result{Predicate: p, Why: "an exchange does not say whether an operation is called"}
	}
	present := 0
	for at, d := range documents {
		value, carried := d[p.Element]
		if !carried {
			continue
		}
		present++
		if why := violates(p, value); why != "" {
			return Result{Predicate: p, Decided: true,
				Why: fmt.Sprintf("exchange %d: %s", at+1, why)}
		}
	}
	if present == 0 {
		switch p.Kind {
		case gatepolicy.PredicateRead, gatepolicy.PredicateSent, gatepolicy.PredicatePopulated,
			gatepolicy.PredicateUnit:
			return Result{Predicate: p, Decided: true,
				Why: fmt.Sprintf("no observed exchange carries %s at all", p.Element)}
		default:
			// A domain and a range are about the values an element takes, so an
			// element no exchange carried is a predicate nothing exercised.
			return Result{Predicate: p,
				Why: fmt.Sprintf("no observed exchange carries %s, so nothing exercised this", p.Element)}
		}
	}
	return Result{Predicate: p, Decided: true, Held: true}
}

// violates is what one value does to one predicate, in words, and empty where the
// value is what the predicate asserts. A value that is neither a string nor a
// number is a violation of every kind that reads one: the form is flat here, and a
// predicate over a nested value is an assertion this platform cannot decide.
func violates(p Predicate, value any) string {
	switch p.Kind {
	case gatepolicy.PredicateRead:
		return ""
	case gatepolicy.PredicateUnit:
		if !carriesUnit(p.Element, p.Argument) {
			return fmt.Sprintf("the name %s does not carry the unit %s", p.Element, p.Argument)
		}
		return ""
	case gatepolicy.PredicatePopulated, gatepolicy.PredicateSent:
		if p.Kind == gatepolicy.PredicateSent && p.Argument == LeftOut {
			return p.Element + " is carried and this consumer leaves it out"
		}
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
	case gatepolicy.PredicateDomain, gatepolicy.PredicateSentDomain:
		text, ok := value.(string)
		if !ok {
			return fmt.Sprintf("%s is %v, which is not one of the names a domain is written in", p.Element, value)
		}
		if contains(Domain(p.Argument), text) {
			return ""
		}
		return fmt.Sprintf("%s is %q, outside the domain %s", p.Element, text, p.Argument)
	case gatepolicy.PredicateRange, gatepolicy.PredicateSentRange:
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
