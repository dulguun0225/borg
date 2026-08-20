package contractcheck

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/declaration"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/release"
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
	Next   contract.Semver
	Change contract.Change
	// Blocking is what still names each breaking element, one entry per element
	// that anything names. An element nothing names is not here.
	Blocking []Blocking
}

// Blocking is one breaking element and everything that still depends on it: the
// declarations in force, the pinned predicates, and — on a store — the service's own
// past. The first two are the deprecation list for that element, which is what
// "without the migration already shipped ahead of it" is: the list having emptied is
// the migration.
//
// Past is the third and it is the one that no list empties. A store promises
// forward, so an element the running build writes and the restored build does not is
// a break the store's own consumer cannot migrate away from — that consumer is a
// release that already exists. What clears it is a form the restored build can read,
// which is why the first item of a store migration adds the new form beside the old
// rather than instead of it, and adds it optional.
type Blocking struct {
	Element    string
	Predicates []declaration.Predicate
	Pinned     []policy.PinnedPredicate
	// Past is the release the store's own past is — the one a rollback would
	// restore — and is empty on every element but a store's forward-breaking one.
	Past string
}

// Blocked reports whether anything at all still depends on the element.
func (b Blocking) Blocked() bool {
	return len(b.Predicates) > 0 || len(b.Pinned) > 0 || b.Past != ""
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

// Unsatisfied is one consumer predicate in force that the candidate's own run does
// not satisfy: the predicate, and what deciding it against the observed exchange
// found.
type Unsatisfied struct {
	Predicate declaration.Predicate
	Result    declaration.Result
}

// Unmet is one predicate this candidate newly declares that its producer's newest
// form does not offer. It is the other side of the same race: a consumer that
// newly declares an element the producer is part-way through removing fails at its
// own gate, and the producer's removal candidate fails at its.
type Unmet struct {
	Draft  declaration.Draft
	Result declaration.Result
}

// Checked is everything enforcement found about one candidate. It is one value
// rather than four calls because the merge row asks all of it at one moment: both
// baselines are computed at the firing and neither is written down, so a caller
// that asked in four calls could see a baseline move between two of them.
type Checked struct {
	Candidate Candidate
	// Publishes is what the candidate's build publishes, which is what the merge
	// queue writes if this candidate merges.
	Publishes []contract.Form
	// Declares is what the candidate's build declares about what it reads.
	Declares []declaration.Draft
	// Broken is every contract this candidate changes, breaking or not.
	Broken []Broken
	// Unsatisfied and Unmet are the two declaration checks.
	Unsatisfied []Unsatisfied
	Unmet       []Unmet
	// Affected is every service that declares it consumes something this
	// candidate publishes, whether or not this candidate breaks it. It is what
	// feeds the risk score's context factor for a human reading the row, and it is
	// the query the design says answers "what does this break".
	Affected []string
	// Observed is how many exchange documents the candidate's run wrote, which is
	// what every predicate here was decided against. Nothing observed and a
	// declaration to decide is a failure, and this is where a reader sees which.
	Observed int
}

// Passed reports whether the candidate may merge as far as its contracts are
// concerned.
func (c Checked) Passed() bool { return c.Check() == "" }

// Check is which mechanical check rejected, in the words package gate names one
// by, and empty where none did. A pinned predicate is told apart from a derived
// declaration because an owner placed it: what a reader of that rejection needs is
// the pin and its author, and what clears it is a withdrawal rather than a release.
func (c Checked) Check() string {
	for _, broken := range c.Broken {
		for _, blocking := range broken.Blocking {
			if len(blocking.Predicates) > 0 || blocking.Past != "" {
				return gate.AutoRejectedByContractDiff
			}
		}
	}
	for _, broken := range c.Broken {
		for _, blocking := range broken.Blocking {
			if len(blocking.Pinned) > 0 {
				return gate.AutoRejectedByPinnedPredicate
			}
		}
	}
	if len(c.Unsatisfied) > 0 || len(c.Unmet) > 0 {
		return gate.AutoRejectedByDeclaration
	}
	return ""
}

// Why is what the rejection says, in words a human reads on the closing row and
// an author reads as feedback. It names the consumer a break would reach, which is
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
			for _, pinned := range blocking.Pinned {
				said = append(said, fmt.Sprintf(
					"%s.%s is %s and pin %s, placed by %s %s, still asserts %s on it",
					broken.Contract.Name, blocking.Element, changeTo(broken.Change, blocking.Element),
					pinned.PinID, pinned.Actor.Kind, pinned.Actor.Name, pinned.Kind))
			}
			if blocking.Past != "" {
				said = append(said, fmt.Sprintf(
					"%s.%s is %s, and this store's consumer is release %s — the one a rollback restores, which does not write it",
					broken.Contract.Name, blocking.Element, changeTo(broken.Change, blocking.Element), blocking.Past))
			}
		}
	}
	for _, unsatisfied := range c.Unsatisfied {
		said = append(said, fmt.Sprintf("%s declares %s and the candidate's run does not satisfy it: %s",
			unsatisfied.Predicate.ServiceID, unsatisfied.Predicate.Describe(), unsatisfied.Result.Why))
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
	case slices.Contains(change.Added, element):
		return "added and always populated, which a store's forward promise does not allow"
	default:
		return "changed"
	}
}

// describe is one consumer's predicates on one element, in the words a rejection
// carries.
func describe(predicates []declaration.Predicate, consumer string) string {
	var said []string
	for _, p := range predicates {
		if p.ServiceID == consumer {
			said = append(said, p.Describe())
		}
	}
	return strings.Join(said, ", ")
}

// Enforce is the whole of what the merge row asks about one candidate's
// contracts: the producer's own diff against the version its current release
// publishes, every consumer's declaration in force decided against the candidate's
// own run, and the candidate's own new declarations decided against each
// producer's newest form.
//
// Both of the design's baselines are here and they are different on purpose. A
// producer's diff runs against what is running, because the promise is to what
// serves it; a declaration runs against the producer's newest release, because that
// is what the consumer will meet. Neither is written down and neither produces a
// record: both are computed at the moment this is called, and the queue calls it
// again at re-verification with itself as the actor — so the race between the two
// resolves either way round it happens.
//
// production is the environment whose deploy records say what is running. It is
// production's and never a candidate's: a candidate environment is a place where
// nothing is current.
func (c *Check) Enforce(ctx context.Context, candidate Candidate, production string) (Checked, error) {
	if err := candidate.validate(); err != nil {
		return Checked{}, err
	}
	if production == "" {
		return Checked{}, fmt.Errorf("%w: it names no production environment", ErrCandidateIncomplete)
	}
	checked := Checked{Candidate: candidate}

	catalog, err := c.policy.PredicateCatalog(ctx)
	if err != nil {
		return Checked{}, err
	}
	checked.Publishes, err = c.checkout.Publishes(ctx, candidate)
	if err != nil {
		return Checked{}, err
	}
	checked.Declares, err = c.checkout.Declares(ctx, candidate, catalog.List)
	if err != nil {
		return Checked{}, err
	}

	// What is running, which is what the producer's own diff is against. A
	// service running nothing has made no promise anything is keeping, so there is
	// no baseline and everything the candidate publishes is an addition.
	var currentNumber int64
	current, running, err := deploy.Current(ctx, c.pool, candidate.ServiceID, production)
	if err != nil {
		return Checked{}, err
	}
	if running {
		rel, err := release.Get(ctx, c.pool, current.ReleaseID)
		if err != nil {
			return Checked{}, err
		}
		currentNumber = rel.Number
	}

	binding, _, err := c.Binding(ctx, candidate.ServiceID)
	if err != nil {
		return Checked{}, err
	}
	for _, p := range binding {
		if !slices.Contains(checked.Affected, p.ServiceID) {
			checked.Affected = append(checked.Affected, p.ServiceID)
		}
	}

	for _, form := range checked.Publishes {
		broken, err := c.diff(ctx, candidate, form, current, currentNumber, running, binding)
		if err != nil {
			return Checked{}, err
		}
		checked.Broken = append(checked.Broken, broken)
	}

	// The consumers' declarations, decided against the candidate's own run. Only
	// the ones naming a contract this candidate publishes: a predicate about an
	// interface this build does not publish is one this candidate cannot have
	// broken.
	naming := namingAnyOf(binding, checked.Publishes)
	if len(naming) > 0 {
		documents, err := c.exchanges.Observed(ctx, candidate)
		if err != nil {
			return Checked{}, err
		}
		checked.Observed = len(documents)
		for _, p := range naming {
			result := p.AgainstExchange(documents)
			if result.Decided && !result.Held {
				checked.Unsatisfied = append(checked.Unsatisfied, Unsatisfied{Predicate: p, Result: result})
			}
		}
	}

	// This candidate's own declarations, against the form its producer's newest
	// release publishes. Only the kinds a form can decide: a domain and a range are
	// about values, and what decides those is the producer's own next candidate.
	for _, draft := range checked.Declares {
		unmet, found, err := c.againstNewest(ctx, candidate, draft)
		if err != nil {
			return Checked{}, err
		}
		if found {
			checked.Unmet = append(checked.Unmet, unmet)
		}
	}
	return checked, nil
}

// diff is one form against the version the service's current release publishes.
func (c *Check) diff(ctx context.Context, candidate Candidate, form contract.Form,
	current deploy.Deploy, currentNumber int64, running bool, binding []declaration.Predicate) (Broken, error) {
	existing, found, err := contract.ByName(ctx, c.pool, candidate.ServiceID, form.Name)
	if err != nil {
		return Broken{}, err
	}
	broken := Broken{Contract: existing, Next: contract.FirstVersion}
	if !found {
		// A contract nothing has published yet: this candidate would create it,
		// so there is no form to diff against and nothing to break.
		broken.Contract = contract.Contract{ServiceID: candidate.ServiceID, Name: form.Name, Kind: form.Kind}
		broken.Change = contract.Diff(contract.Form{}, form)
		return broken, nil
	}

	before := contract.Form{}
	if running {
		version, hadOne, err := contract.VersionAt(ctx, c.pool, existing.ID, currentNumber)
		if err != nil {
			return Broken{}, err
		}
		if hadOne {
			before, err = contract.FormOf(ctx, c.pool, existing, version.ID)
			if err != nil {
				return Broken{}, err
			}
			broken.From, broken.Had = version.Semver, true
		}
	}
	broken.Change = contract.Diff(before, form)
	broken.Next = contract.FirstVersion
	if broken.Had {
		broken.Next = broken.From.Next(len(broken.Change.Breaking) > 0)
	}

	// The deprecation list for each breaking element: the declarations in force
	// naming it, and any pinned predicate naming it. Empty is the migration having
	// shipped ahead of the change, which is exactly what this check is for.
	subjects := make([]string, 0, len(broken.Change.Breaking))
	for _, element := range broken.Change.Breaking {
		subjects = append(subjects, contract.ElementSubject(existing.ID, element))
	}
	pinned, err := c.policy.PinnedPredicatesOn(ctx, subjects)
	if err != nil {
		return Broken{}, err
	}
	for _, element := range broken.Change.Breaking {
		blocking := Blocking{
			Element:    element,
			Predicates: declaration.NamingElement(binding, candidate.ServiceID, existing.Name, element),
		}
		for _, p := range pinned {
			if p.Subject == contract.ElementSubject(existing.ID, element) {
				blocking.Pinned = append(blocking.Pinned, p)
			}
		}
		// A store's forward-breaking element is blocked by the release a rollback
		// would restore, which exists whenever there is a form to diff against at
		// all. There is no list to empty here: the consumer is a release that has
		// already shipped.
		if existing.Kind.Forward() && slices.Contains(broken.Change.Added, element) {
			blocking.Past = current.ReleaseID
		}
		if blocking.Blocked() {
			broken.Blocking = append(broken.Blocking, blocking)
		}
	}
	return broken, nil
}

// againstNewest decides one of this candidate's own declarations against the form
// its producer's newest release publishes. It reports only a failure, because a
// predicate that held and one a form cannot decide are both the candidate passing
// here.
//
// A producer's service or interface the factory has never seen published decides
// nothing: a contract exists only from the merge that first published it, so a
// consumer declaring ahead of its producer is not a consumer whose assumption has
// been broken. What covers that case is the producer's own next candidate, which
// will find the declaration in force against it.
func (c *Check) againstNewest(ctx context.Context, candidate Candidate, draft declaration.Draft) (Unmet, bool, error) {
	if !draft.Kind.DecidableAgainstAForm() || draft.ProducerServiceID == "" {
		return Unmet{}, false, nil
	}
	producer, found, err := contract.ByName(ctx, c.pool, draft.ProducerServiceID, draft.Interface)
	if err != nil || !found {
		return Unmet{}, false, err
	}
	version, hasOne, err := contract.NewestVersion(ctx, c.pool, producer.ID)
	if err != nil || !hasOne {
		return Unmet{}, false, err
	}
	form, err := contract.FormOf(ctx, c.pool, producer, version.ID)
	if err != nil {
		return Unmet{}, false, err
	}
	result := declaration.Predicate{
		ItemID: candidate.ItemID, ServiceID: candidate.ServiceID, ArtifactID: candidate.BuildID,
		ProducerService: draft.ProducerService, ProducerServiceID: draft.ProducerServiceID,
		Interface: draft.Interface, Element: draft.Element,
		Kind: draft.Kind, Argument: draft.Argument,
	}.AgainstForm(form)
	if result.Decided && !result.Held {
		return Unmet{Draft: draft, Result: result}, true, nil
	}
	return Unmet{}, false, nil
}

// namingAnyOf is the predicates among these that name one of these forms, which
// is what a candidate publishing them could have broken.
func namingAnyOf(predicates []declaration.Predicate, forms []contract.Form) []declaration.Predicate {
	var naming []declaration.Predicate
	for _, p := range predicates {
		for _, form := range forms {
			if p.Interface == form.Name {
				naming = append(naming, p)
				break
			}
		}
	}
	return naming
}
