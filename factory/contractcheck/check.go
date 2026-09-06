package contractcheck

import (
	"context"
	"fmt"
	"slices"

	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/release"
)

// Broken, Blocking, Unsatisfied, Unmet and Checked — what enforcement found
// about one candidate, and how it reads as a rejection — are in checked.go.

// Enforce is the whole of what the merge row asks about one candidate's contracts:
// the producer's own diff against the version its current release publishes, every
// consumer contract in force decided against the candidate's own run, this
// candidate's own new consumer contracts decided against each producer's newest
// form, and the store rule over every store contract it publishes.
//
// Both of the design's baselines are here and they are different on purpose. A
// producer's diff runs against what is running, because the promise is to what
// serves it; a consumer contract runs against the producer's newest release,
// because that is what the consumer will meet. Neither is written down and neither
// produces a record: both are computed at the moment this is called, and the queue
// calls it again at re-verification with itself as the actor — so the race between
// the two resolves either way round it happens.
//
// A consumer contract's two sides are decided against two things, because
// compatibility runs the other way for an input: what a consumer reads is decided
// against the candidate's run, and what it sends is decided against the request
// form the candidate publishes.
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

	allowed, err := c.policy.AllowedPredicateKinds(ctx)
	if err != nil {
		return Checked{}, err
	}
	checked.Publishes, err = c.checkout.Publishes(ctx, candidate)
	if err != nil {
		return Checked{}, err
	}
	checked.Declares, err = c.checkout.Declares(ctx, candidate, allowed.List)
	if err != nil {
		return Checked{}, err
	}

	// What is running, which is what the producer's own diff is against. A
	// service running nothing has made no promise anything is keeping, so there is
	// no baseline and everything the candidate publishes is an addition.
	prodEnv, err := environment.Get(ctx, c.pool, production)
	if err != nil {
		return Checked{}, err
	}
	var currentNumber int64
	current, running, err := deploy.Current(ctx, c.pool, candidate.ServiceID, production, prodEnv.Addresses())
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

	if err := c.decideConsumers(ctx, candidate, &checked, binding); err != nil {
		return Checked{}, err
	}

	// This candidate's own consumer contracts, against the form its producer's
	// newest release publishes. A run that could not derive declares nothing, and
	// what it does instead is put a human at the gate — which is the score's
	// reading of the record and not a rejection here.
	for _, draft := range checked.Declares.Drafts {
		unmet, found, err := c.againstNewest(ctx, candidate, draft)
		if err != nil {
			return Checked{}, err
		}
		if found {
			checked.Unmet = append(checked.Unmet, unmet)
		}
	}

	if err := c.storeRule(ctx, candidate, &checked, binding); err != nil {
		return Checked{}, err
	}
	return checked, nil
}

// decideConsumers decides every consumer contract in force naming something this
// candidate publishes. Which side a predicate is on says what decides it: what a
// consumer receives is decided against the candidate's run, what it sends against
// the request form the candidate publishes, and everything about a store against
// the state the candidate's environment holds.
func (c *Check) decideConsumers(ctx context.Context, candidate Candidate, checked *Checked,
	binding []consumercontract.Predicate) error {
	var documents []consumercontract.Document
	fetched := false
	for _, form := range checked.Publishes {
		naming := consumercontract.AgainstInterface(binding, candidate.ServiceID, form.Name)
		if len(naming) == 0 {
			continue
		}
		if form.Kind == contract.KindStore {
			// Every store declaration in force, read and write, is decided
			// against the state the candidate's own environment holds. That is
			// what decides the middle items of a migration, whose diff is empty
			// by construction.
			rows, err := c.store.Rows(ctx, candidate, form.Name)
			if err != nil {
				return err
			}
			for _, p := range naming {
				checked.unsatisfied(p.AgainstExchange(rows))
			}
			continue
		}
		if !fetched {
			observed, err := c.exchanges.Observed(ctx, candidate)
			if err != nil {
				return err
			}
			documents, fetched = observed, true
			checked.Observed = len(observed)
		}
		for _, p := range naming {
			if p.Kind.Side() == gatepolicy.SideSent {
				// What a consumer sends is decided against the request form this
				// candidate publishes, compatibility running the other way for
				// an input.
				checked.unsatisfied(p.AgainstForm(form))
				continue
			}
			checked.unsatisfied(p.AgainstExchange(documents))
		}
	}
	return nil
}

// unsatisfied records one decided predicate that did not hold, and one nothing
// decided: undecided is read at the gate the way a failure is.
func (c *Checked) unsatisfied(result consumercontract.Result) {
	if result.Decided && result.Held {
		return
	}
	c.Unsatisfied = append(c.Unsatisfied, Unsatisfied{Predicate: result.Predicate, Result: result})
}

// diff is one form against the version the service's current release publishes.
func (c *Check) diff(ctx context.Context, candidate Candidate, form contract.Form,
	current deploy.Deploy, currentNumber int64, running bool, binding []consumercontract.Predicate) (Broken, error) {
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
		broken.Before = before
	}
	broken.Change = contract.Diff(before, form)
	broken.Next = contract.FirstVersion
	if broken.Had {
		broken.Next = broken.From.Next(len(broken.Change.Breaking) > 0)
	}

	// The deprecation list for each breaking element: the consumer contracts in
	// force naming it, and any safeguard's predicate naming it. Empty is the
	// migration having shipped ahead of the change, which is exactly what this
	// check is for.
	subjects := make([]string, 0, len(broken.Change.Breaking))
	for _, element := range broken.Change.Breaking {
		subjects = append(subjects, contract.ElementSubject(existing.ID, element))
	}
	safeguards, err := c.policy.SafeguardPredicatesOn(ctx, subjects)
	if err != nil {
		return Broken{}, err
	}
	for _, element := range broken.Change.Breaking {
		naming := consumercontract.NamingElement(binding, candidate.ServiceID, existing.Name, element)
		if ordinaryConstraint(existing.Kind, broken.Change, before, form, element) {
			// The store rule's ordinary path: a not-null constraint or a domain
			// check on the new form is addable where every declaration in force
			// writes the form populated and inside the constraint's domain. So
			// what holds it is a declaration the new form rejects, not the
			// existence of one — the backfill's completion is the other half and
			// is the store rule's.
			naming = rejectedBy(naming, form)
		}
		blocking := Blocking{Element: element, Predicates: naming}
		for _, p := range safeguards {
			if p.Subject == contract.ElementSubject(existing.ID, element) {
				blocking.Safeguards = append(blocking.Safeguards, p)
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

// ordinaryConstraint reports whether a store element breaks for the one reason
// the design gives an ordinary path: a not-null constraint or a domain check
// added to the form. Everything else a store element can do — a removal, a
// retype, a weakening, an element added and always populated — is destructive
// against any declaration in force, and so is a uniqueness rule, which is not on
// the design's addable list because no predicate can say a write would not
// collide with another.
func ordinaryConstraint(kind contract.Kind, change contract.Change, before, after contract.Form, element string) bool {
	if kind != contract.KindStore {
		return false
	}
	if !slices.Contains(change.Constrained, element) && !slices.Contains(change.Narrowed, element) {
		return false
	}
	for _, otherwise := range [][]string{
		change.Removed, change.Retyped, change.Weakened, change.Required, change.Added,
	} {
		if slices.Contains(otherwise, element) {
			return false
		}
	}
	was, had := before.Element(element)
	is, has := after.Element(element)
	return had && has && !(!was.Unique && is.Unique)
}

// rejectedBy is the predicates among these that the candidate's own form does
// not satisfy, which is what "every declaration in force writes the form
// populated and inside the constraint's domain" comes to. A predicate the form
// cannot decide is not one the constraint rejects: what decides a store
// declaration against the state a run left behind is decideConsumers, and this
// is the diff's question.
func rejectedBy(predicates []consumercontract.Predicate, form contract.Form) []consumercontract.Predicate {
	var rejected []consumercontract.Predicate
	for _, p := range predicates {
		if result := p.AgainstForm(form); result.Decided && !result.Held {
			rejected = append(rejected, p)
		}
	}
	return rejected
}

// againstNewest decides one of this candidate's own consumer contracts against
// the form its producer's newest release publishes. It reports only a failure,
// because a predicate that held and one a form cannot decide are both the
// candidate passing here.
//
// A producer's service or interface the factory has never seen published decides
// nothing: a contract exists only from the merge that first published it, so a
// consumer declaring ahead of its producer is not a consumer whose assumption has
// been broken. What covers that case is the producer's own next candidate, which
// will find the consumer contract in force against it.
func (c *Check) againstNewest(ctx context.Context, candidate Candidate, draft consumercontract.Draft) (Unmet, bool, error) {
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
	result := consumercontract.Predicate{
		ItemID: candidate.ItemID, ServiceID: candidate.ServiceID, ArtifactID: candidate.BuildID,
		Address:         draft.Address,
		ProducerService: draft.ProducerService, ProducerServiceID: draft.ProducerServiceID,
		Interface: draft.Interface, Element: draft.Element,
		Kind: draft.Kind, Argument: draft.Argument,
	}.AgainstForm(form)
	if result.Decided && !result.Held {
		return Unmet{Draft: draft, Result: result}, true, nil
	}
	return Unmet{}, false, nil
}
