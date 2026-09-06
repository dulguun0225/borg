package contractcheck

import (
	"fmt"
	"slices"
	"strings"

	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/policy"
)

// Broken is one contract this candidate's build changes: the contract, the
// version the service's current release publishes, what the diff did, and — per
// element whose change the promise does not allow — what still names it.
//
// A breaking change with nothing naming any of its elements is allowed and is
// recorded here all the same, because that is the migration having shipped ahead of
// it and a reader of the merge row should see which of the two it was.
type Broken struct {
	Contract contract.Contract
	// From is the version the current release publishes, and false in Had where
	// the service is running nothing or has never published this contract — a
	// promise is kept by what serves it, so there is nothing to break.
	From contract.Semver
	Had  bool
	// Next is the version this candidate would mint if it merged.
	Next contract.Semver
	// Before is the form the current release publishes, which is what Change is
	// against. It is here because a mark on an element this candidate removed is
	// read off it: the candidate's own form no longer has that element.
	Before contract.Form
	Change contract.Change
	// Breaks is the elements this candidate actually breaks on, which is what
	// mints Next: [contract.Change.Breaking], which is what breaks whatever any
	// declaration says, plus a store element whose newly added not-null
	// constraint or domain check a declaration in force writes outside. Major
	// means a consumer breaks, so an addable constraint no declaration violates
	// is not here and mints a minor.
	Breaks []string
	// Blocking is what still names each breaking element, one entry per element
	// that anything names. An element nothing names is not here.
	Blocking []Blocking
}

// Blocking is one breaking element and everything that still depends on it: the
// consumer contracts in force, the consumers no derivation shows to have stopped
// reading it, the safeguards' predicates, and — on a store — the service's own
// past. The first three are the deprecation list for that element, which
// is what "without the migration already shipped ahead of it" is: the list having
// emptied is the migration.
//
// Past is the third and it is the one that no list empties. A store promises
// forward, so an element the running build writes and the restored build does not is
// a break the store's own consumer cannot migrate away from — that consumer is a
// release that already exists. What clears it is a form the restored build can read,
// which is why the first item of a store migration adds the new form beside the old
// rather than instead of it, and adds it optional.
type Blocking struct {
	Element    string
	Predicates []consumercontract.Predicate
	// Unreadable is the consumers whose derivation holds the element whatever
	// their predicates say — the partial ones and the ones nobody could read.
	// [Marked] carries the same pair for a marked element, the two being one
	// list: a removal that reaches this row by any route waits on what the
	// detector waits on.
	Unreadable Unreadable
	Safeguards []policy.SafeguardPredicate
	// Past is the release the store's own past is — the one a rollback would
	// restore — and is empty on every element but a store's forward-breaking one.
	Past string
}

// Blocked reports whether anything at all still depends on the element.
func (b Blocking) Blocked() bool {
	return b.HeldByADerivation() || len(b.Safeguards) > 0 || b.Past != ""
}

// HeldByADerivation reports whether the deprecation list for this element is not
// empty: a consumer contract in force names it, or a consumer's derivation
// cannot be read as having stopped naming it. It is what a breaking change waits
// for and what the detector's [Marked.Empty] asks of the same element.
func (b Blocking) HeldByADerivation() bool {
	return len(b.Predicates) > 0 || b.Unreadable.Holds()
}

// Consumers is the distinct consumer services on the list, which is the answer to
// "who does this break".
func (b Blocking) Consumers() []string {
	var services []string
	for _, p := range b.Predicates {
		if !slices.Contains(services, p.ServiceID) {
			services = append(services, p.ServiceID)
		}
	}
	return services
}

// Unsatisfied is one consumer predicate in force that the candidate does not
// satisfy: the predicate, and what deciding it found. A predicate the candidate's
// run did not decide is here too — undecided is read at the Merge to master gate
// the way a failure is, because a predicate decided against nothing passes every
// check the factory has and assures nothing.
type Unsatisfied struct {
	Predicate consumercontract.Predicate
	Result    consumercontract.Result
}

// Undecided reports whether nothing could decide the predicate, which is what a
// reader of the rejection needs to tell from a decided failure.
func (u Unsatisfied) Undecided() bool { return !u.Result.Decided }

// Unmet is one predicate this candidate newly declares that its producer's newest
// form does not offer. It is the other side of the same race: a consumer that
// newly declares an element the producer is part-way through removing fails at its
// own gate, and the producer's removal candidate fails at its.
type Unmet struct {
	Draft  consumercontract.Draft
	Result consumercontract.Result
}

// Checked is everything enforcement found about one candidate. It is one value
// rather than five calls because the merge row asks all of it at one moment: both
// baselines are computed at the firing and neither is written down, so a caller
// that asked in five calls could see a baseline move between two of them.
type Checked struct {
	Candidate Candidate
	// Publishes is what the candidate's build publishes, which is what the merge
	// queue writes if this candidate merges.
	Publishes []contract.Form
	// Declares is what the extractor made of the candidate's build: its
	// predicates, what it could not follow, and whether it could derive at all.
	Declares consumercontract.Derived
	// Broken is every contract this candidate changes, breaking or not.
	Broken []Broken
	// Unreadable is the consumers no derivation shows to have stopped reading
	// anything this candidate publishes, which every element of Blocking carries
	// too. It is here so a reader of the row sees the pair once rather than once
	// per element.
	Unreadable Unreadable
	// Unsatisfied and Unmet are the two consumer contract checks.
	Unsatisfied []Unsatisfied
	Unmet       []Unmet
	// Migrations is what the store rule found about each store contract this
	// candidate publishes.
	Migrations []Migration
	// Affected is every service that declares it consumes something this
	// candidate publishes, whether or not this candidate breaks it. It is what
	// feeds the risk score's context factor for a human reading the row, and it is
	// the query the design says answers "what does this break".
	Affected []string
	// Observed is how many exchange documents the candidate's run wrote, which is
	// what every receive-side predicate here was decided against. Nothing observed
	// leaves every one of them undecided, and this is where a reader sees which.
	Observed int
}

// Passed reports whether the candidate may merge as far as its contracts are
// concerned.
func (c Checked) Passed() bool { return c.Check() == "" }

// Check is which mechanical check rejected, in the words package gate names one
// by, and empty where none did. A safeguard's predicate is told apart from a
// derived consumer contract because an owner placed it: what a reader of that
// rejection needs is the safeguard and its author, and what clears it is a
// withdrawal rather than a release.
func (c Checked) Check() string {
	for _, broken := range c.Broken {
		for _, blocking := range broken.Blocking {
			if blocking.HeldByADerivation() || blocking.Past != "" {
				return gate.AutoRejectedByContractDiff
			}
		}
	}
	for _, migration := range c.Migrations {
		if migration.Blocked() {
			return gate.AutoRejectedByContractDiff
		}
	}
	for _, broken := range c.Broken {
		for _, blocking := range broken.Blocking {
			if len(blocking.Safeguards) > 0 {
				return gate.AutoRejectedBySafeguardPredicate
			}
		}
	}
	if len(c.Unsatisfied) > 0 || len(c.Unmet) > 0 {
		return gate.AutoRejectedByConsumerContract
	}
	return ""
}

// Why is what the rejection says, in words a human reads on the close event and
// an author reads as a reason. It names the consumer a break would reach, which is
// the whole point of the graph answering who is affected.
func (c Checked) Why() string {
	var said []string
	for _, broken := range c.Broken {
		for _, blocking := range broken.Blocking {
			for _, consumer := range blocking.Consumers() {
				said = append(said, fmt.Sprintf(
					"%s.%s is %s and %s still declares it: %s",
					broken.Contract.Name, blocking.Element, changeTo(broken.Change, blocking.Element),
					consumer, describe(blocking.Predicates, consumer)))
			}
			for _, consumer := range blocking.Unreadable.Partial {
				said = append(said, fmt.Sprintf(
					"%s.%s is %s and %s's derivation is partial, so nothing shows it stopped reading it",
					broken.Contract.Name, blocking.Element, changeTo(broken.Change, blocking.Element), consumer))
			}
			for _, consumer := range blocking.Unreadable.CouldNotDerive {
				said = append(said, fmt.Sprintf(
					"%s.%s is %s and nobody could derive %s at all, so nothing bounds what it consumes",
					broken.Contract.Name, blocking.Element, changeTo(broken.Change, blocking.Element), consumer))
			}
			for _, s := range blocking.Safeguards {
				said = append(said, fmt.Sprintf(
					"%s.%s is %s and safeguard %s, placed by %s %s, still asserts %s on it",
					broken.Contract.Name, blocking.Element, changeTo(broken.Change, blocking.Element),
					s.SafeguardID, s.Actor.Kind, s.Actor.Key, s.Kind))
			}
			if blocking.Past != "" {
				said = append(said, fmt.Sprintf(
					"%s.%s is %s, and this store's consumer is release %s — the one a rollback restores, which does not write it",
					broken.Contract.Name, blocking.Element, changeTo(broken.Change, blocking.Element), blocking.Past))
			}
		}
	}
	for _, migration := range c.Migrations {
		said = append(said, migration.Why()...)
	}
	for _, unsatisfied := range c.Unsatisfied {
		what := "the candidate does not satisfy it"
		if unsatisfied.Undecided() {
			what = "nothing decided it, which is read here the way a failure is"
		}
		said = append(said, fmt.Sprintf("%s declares %s and %s: %s",
			unsatisfied.Predicate.ServiceID, unsatisfied.Predicate.Describe(), what, unsatisfied.Result.Why))
	}
	for _, unmet := range c.Unmet {
		said = append(said, fmt.Sprintf("this candidate declares %s.%s.%s and its producer's newest form does not offer it: %s",
			unmet.Draft.ProducerService, unmet.Draft.Interface, unmet.Draft.Element, unmet.Result.Why))
	}
	return strings.Join(said, "; ")
}

// changeTo is what the diff did to one element, in one word, for a rejection
// message. It reads the lists rather than being carried beside them, so the words
// and the diff cannot disagree.
func changeTo(change contract.Change, element string) string {
	switch {
	case slices.Contains(change.Removed, element):
		return "removed"
	case slices.Contains(change.Retyped, element):
		return "retyped"
	case slices.Contains(change.Weakened, element):
		return "no longer always populated"
	case slices.Contains(change.Required, element):
		return "now required, which every caller that does not send it breaks on"
	case slices.Contains(change.Narrowed, element):
		return "narrowed, which a caller sending what it sent no longer fits"
	case slices.Contains(change.Constrained, element):
		return "newly constrained, which a write declared inside the old range violates"
	case slices.Contains(change.Added, element):
		return "added and always populated, which a store's forward promise does not allow"
	default:
		return "changed"
	}
}

// describe is one consumer's predicates on one element, in the words a rejection
// carries.
func describe(predicates []consumercontract.Predicate, consumer string) string {
	var said []string
	for _, p := range predicates {
		if p.ServiceID == consumer {
			said = append(said, p.Describe())
		}
	}
	return strings.Join(said, ", ")
}
