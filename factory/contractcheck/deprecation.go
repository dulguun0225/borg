package contractcheck

import (
	"context"
	"fmt"
	"slices"

	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/service"
)

// Marked is one deprecation-marked element and the list that waits on it: the
// consumers whose consumer contracts in force still name it, the consumers whose
// contracts in force over that producer are partial, the consumers nobody could
// derive at all, plus any safeguard's predicate naming it.
//
// Nothing writes this. It is computed, so it cannot go stale: a consumer that
// stops reading the element leaves the list because its next release derives
// nothing, with nobody remembering to remove it. A maintained list would need two
// writers to agree about a fact both read out of records that already say it.
type Marked struct {
	Contract contract.Contract
	// ServiceName is the publisher's name, for the words a statement and a
	// printed list are read in.
	ServiceName string
	// Version is the newest version of the contract, which is the one whose form
	// the mark is read out of.
	Version contract.Version
	Element contract.Element
	// Predicates is the consumer contracts in force naming the element, whichever
	// consumer.
	Predicates []consumercontract.Predicate
	// Partial is the services whose consumer contracts in force over this
	// producer are partial: the extractor met something it could not follow, and
	// the record's silence on an element never shows that the consumer does not
	// touch it.
	Partial []string
	// CouldNotDerive is the services whose consumer contracts in force could not
	// be derived at all. Nothing bounds what an unreadable consumer consumes, so
	// one is on every producer's list.
	CouldNotDerive []string
	// Safeguards is the safeguards' predicates naming it, which an owner placed
	// and a derivation did not.
	Safeguards []policy.SafeguardPredicate
}

// Empty reports whether every consumer the factory can read has migrated off the
// element, which is the condition the brownout is raised on. A partial record and
// a could-not-derive record hold the list open: the first is a consumer whose
// reading the extractor could not follow, and the second one nobody could read at
// all, and neither can be known to have stopped reading it.
//
// A safeguard is deliberately not part of it: a safeguard never stops the item
// existing, only passing — the removal candidate is rejected at its Merge to master
// gate on the safeguard, counts an attempt, and appears as an escalation naming the
// safeguard and its author, which is the blocked removal item asking the consumer
// to confirm.
func (m Marked) Empty() bool {
	return len(m.Predicates) == 0 && len(m.Partial) == 0 && len(m.CouldNotDerive) == 0
}

// Consumers is the distinct services whose consumer contracts in force still
// name the element, in the order the predicates came back.
func (m Marked) Consumers() []string {
	var services []string
	for _, p := range m.Predicates {
		if !slices.Contains(services, p.ServiceID) {
			services = append(services, p.ServiceID)
		}
	}
	return services
}

// BrownoutStatement is the statement the brownout intent carries. What
// deduplicates two passes over one marked element is the evidence intake keys the
// intent on — the contract and the element — and not this statement, so what it
// names is chosen for the human reading it and the spec author it is given to.
func BrownoutStatement(serviceName, contractName, element string) string {
	return fmt.Sprintf(
		"Run a brownout of the deprecated element %s of contract %s of service %s: disable it in behaviour, leaving the form unchanged.",
		element, contractName, serviceName)
}

// RemovalStatement is the statement the removal intent carries, on the same terms.
func RemovalStatement(serviceName, contractName, element string) string {
	return fmt.Sprintf("Remove the deprecated element %s from contract %s of service %s.",
		element, contractName, serviceName)
}

// Deprecated is every deprecation-marked element in the factory with the list
// that waits on it, contract by contract. The mark is read off the newest version
// of each contract, which is where the producer's own item put it; a contract with
// no version and one whose newest form marks nothing are both absent from the
// answer.
func (c *Check) Deprecated(ctx context.Context) ([]Marked, error) {
	contracts, err := contract.All(ctx, c.pool)
	if err != nil {
		return nil, err
	}
	blind, err := c.couldNotDerive(ctx)
	if err != nil {
		return nil, err
	}
	var marked []Marked
	for _, con := range contracts {
		version, hasOne, err := contract.NewestVersion(ctx, c.pool, con.ID)
		if err != nil {
			return nil, err
		}
		if !hasOne {
			continue
		}
		form, err := contract.FormOf(ctx, c.pool, con, version.ID)
		if err != nil {
			return nil, err
		}
		elements := form.Marked()
		if len(elements) == 0 {
			continue
		}
		svc, err := service.Get(ctx, c.pool, con.ServiceID)
		if err != nil {
			return nil, err
		}
		binding, ranges, err := c.Binding(ctx, con.ServiceID)
		if err != nil {
			return nil, err
		}
		partial, err := c.partial(ctx, ranges)
		if err != nil {
			return nil, err
		}
		subjects := make([]string, 0, len(elements))
		for _, element := range elements {
			subjects = append(subjects, contract.ElementSubject(con.ID, element))
		}
		safeguards, err := c.policy.SafeguardPredicatesOn(ctx, subjects)
		if err != nil {
			return nil, err
		}
		for _, name := range elements {
			element, _ := form.Element(name)
			one := Marked{
				Contract: con, ServiceName: svc.Name, Version: version, Element: element,
				Predicates:     consumercontract.NamingElement(binding, con.ServiceID, con.Name, name),
				Partial:        partial,
				CouldNotDerive: blind,
			}
			for _, p := range safeguards {
				if p.Subject == contract.ElementSubject(con.ID, name) {
					one.Safeguards = append(one.Safeguards, p)
				}
			}
			marked = append(marked, one)
		}
	}
	return marked, nil
}

// partial is the services among these ranges whose consumer contract in force is
// partial. The range is per consumer, so what is asked of each is the derivation of
// the newest consumer contract version of each item in its range.
func (c *Check) partial(ctx context.Context, ranges []InForce) ([]string, error) {
	var services []string
	for _, in := range ranges {
		derivations, err := consumercontract.DerivationsForItems(ctx, c.pool, in.ItemIDs)
		if err != nil {
			return nil, err
		}
		for _, d := range derivations {
			if d.Partial() && !slices.Contains(services, in.ServiceID) {
				services = append(services, in.ServiceID)
			}
		}
	}
	return services, nil
}

// couldNotDerive is every service whose consumer contract in force could not be
// derived at all. Nothing bounds what an unreadable consumer consumes, so one is on
// every producer's deprecation list — which is why this is asked of every service
// and not of the ones that have declared against a particular producer.
func (c *Check) couldNotDerive(ctx context.Context) ([]string, error) {
	services, err := service.All(ctx, c.pool)
	if err != nil {
		return nil, err
	}
	var blind []string
	for _, svc := range services {
		in, err := c.ConsumerContractsInForce(ctx, svc.ID)
		if err != nil {
			return nil, err
		}
		derivations, err := consumercontract.DerivationsForItems(ctx, c.pool, in.ItemIDs)
		if err != nil {
			return nil, err
		}
		for _, d := range derivations {
			if d.CouldNotDerive() && !slices.Contains(blind, svc.ID) {
				blind = append(blind, svc.ID)
			}
		}
	}
	return blind, nil
}

// Raised is what one pass of the detector did about one marked element: the
// element, which raise it is, the intent that asks for it, and whether this pass
// took it in or found one already waiting.
type Raised struct {
	Marked Marked
	// Brownout is whether this is the brownout raise; a removal raise is the
	// other, and no pass makes both for one element.
	Brownout bool
	Intent   intent.Intent
	// New is false where an intent on the same evidence was already there and not
	// finished, which is what stops a second raise arriving beside one already
	// raised.
	New bool
}

// Raise is the detector: one pass over every marked element, raising the brownout
// where the derived consumer contracts naming the element are gone, and the
// removal where that brownout's window has reached its cap uncrossed having
// received volume. Nobody has to remember the last two items of a migration.
//
// The two raises are keyed on one evidence — the contract and the element — which
// is what stops a second brownout arriving beside one already raised. They cannot
// collide: a removal is raised only on a brownout whose window has closed, and an
// intent on the same evidence that has not finished is that intent, whatever state
// the interview or the decomposition has moved it to since.
//
// It raises neither again for an element whose brownout failed: the failed window
// stands in the record as a consumer read the derivation missed, and both raises
// are a human's from then on.
//
// A factory composed with no intake raises nothing and is not an error: what it
// loses is the detector, which is the one thing in this component that writes.
func (c *Check) Raise(ctx context.Context) ([]Raised, error) {
	if c.intake == nil {
		return nil, nil
	}
	marked, err := c.Deprecated(ctx)
	if err != nil {
		return nil, err
	}
	var raised []Raised
	for _, m := range marked {
		if !m.Empty() {
			continue
		}
		brownout, err := c.brownout(ctx, m)
		if err != nil {
			return nil, err
		}
		switch {
		case brownout.Failed:
			// The failed brownout stands in the record as a consumer read the
			// derivation missed, so raising this element's brownout or removal
			// again is from then on a human's act, never the detector's.
			continue
		case !brownout.Ran:
			one, err := c.raise(ctx, m, true, BrownoutStatement(m.ServiceName, m.Contract.Name, m.Element.Name))
			if err != nil {
				return nil, err
			}
			raised = append(raised, one)
		case brownout.Establishes:
			one, err := c.raise(ctx, m, false, RemovalStatement(m.ServiceName, m.Contract.Name, m.Element.Name))
			if err != nil {
				return nil, err
			}
			raised = append(raised, one)
		}
	}
	return raised, nil
}

// raise takes one intent in on the element's evidence, or reports the one already
// waiting on it.
func (c *Check) raise(ctx context.Context, m Marked, brownout bool, statement string) (Raised, error) {
	evidence := intent.Evidence{ContractID: m.Contract.ID, Element: m.Element.Name}
	waiting, found, err := intent.OnEvidence(ctx, c.pool, evidence)
	if err != nil {
		return Raised{}, err
	}
	if found {
		return Raised{Marked: m, Brownout: brownout, Intent: waiting}, nil
	}
	taken, err := c.intake.TakeIn(ctx, Actor, intent.Arrival{
		Source:    intent.SourceDetector,
		Statement: statement,
		Evidence:  evidence,
	})
	if err != nil {
		return Raised{}, err
	}
	return Raised{Marked: m, Brownout: brownout, Intent: taken, New: true}, nil
}
