package contractcheck

import (
	"context"
	"fmt"
	"slices"

	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/declaration"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/service"
)

// Marked is one deprecation-marked element and the list that waits on it: the
// consumers whose declarations in force still name it, plus any pinned predicate
// naming it.
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
	// Predicates is the declarations in force naming the element, whichever
	// consumer.
	Predicates []declaration.Predicate
	// Pinned is the pinned predicates naming it, which an owner placed and a
	// derivation did not.
	Pinned []policy.PinnedPredicate
}

// Empty reports whether the derived declarations naming the element are gone,
// which is the condition the removal intent is raised on. A pin is deliberately
// not part of it: a pin never stops the item existing, only passing — the removal
// candidate is rejected at its merge gate on the pin, counts an attempt, and
// appears as an escalation naming the pin and its author, which is the blocked
// removal item asking the consumer to confirm.
func (m Marked) Empty() bool { return len(m.Predicates) == 0 }

// Consumers is the distinct services whose declarations in force still name the
// element, in the order the predicates came back.
func (m Marked) Consumers() []string {
	var services []string
	for _, p := range m.Predicates {
		if !slices.Contains(services, p.ServiceID) {
			services = append(services, p.ServiceID)
		}
	}
	return services
}

// RemovalStatement is the statement the removal intent carries. It is derived
// from the contract and the element and from nothing else, so two passes over one
// marked element produce one statement and the second finds the first's intent
// already waiting — which is how the detector is deduplicated without a record
// saying it has fired.
//
// It names the service and the contract by name rather than by id, because a
// statement is what a human reads and what the spec author is given. What that
// costs is what every statement-as-handle costs here: an owner who types this
// sentence character for character gets the detector's intent rather than one of
// their own.
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
		binding, _, err := c.Binding(ctx, con.ServiceID)
		if err != nil {
			return nil, err
		}
		subjects := make([]string, 0, len(elements))
		for _, element := range elements {
			subjects = append(subjects, contract.ElementSubject(con.ID, element))
		}
		pinned, err := c.policy.PinnedPredicatesOn(ctx, subjects)
		if err != nil {
			return nil, err
		}
		for _, name := range elements {
			element, _ := form.Element(name)
			one := Marked{
				Contract: con, ServiceName: svc.Name, Version: version, Element: element,
				Predicates: declaration.NamingElement(binding, con.ServiceID, con.Name, name),
			}
			for _, p := range pinned {
				if p.Subject == contract.ElementSubject(con.ID, name) {
					one.Pinned = append(one.Pinned, p)
				}
			}
			marked = append(marked, one)
		}
	}
	return marked, nil
}

// Raised is what one pass of the detector did about one marked element: the
// element, the intent that asks for the removal, and whether this pass took it in
// or found one already waiting.
type Raised struct {
	Marked Marked
	Intent intent.Intent
	// New is false where an unrefined intent with the same statement was already
	// there, which is what stops a second pass raising a second intent for one
	// element.
	New bool
}

// RaiseRemovals is the detector: one pass over every marked element, taking a
// removal intent in for each whose derived declarations are gone. Nobody has to
// remember step three of a migration.
//
// It is deduplicated by the statement rather than by a record saying the detector
// has fired. An unrefined intent with the same statement is that intent, and a
// pass that finds one takes nothing in — which is the handle a revert already
// reaches the pipeline by, and costs what that costs. Once the intent has been cut,
// it is no longer unrefined and this pass would raise a second one; what stops
// that is the element leaving the newest form when the removal ships, which is the
// same event that makes the removal unnecessary.
//
// A factory composed with no intake raises nothing and is not an error: what it
// loses is the detector, which is the one thing in this component that writes.
func (c *Check) RaiseRemovals(ctx context.Context) ([]Raised, error) {
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
		statement := RemovalStatement(m.ServiceName, m.Contract.Name, m.Element.Name)
		waiting, found, err := intent.Unrefined(ctx, c.pool, statement)
		if err != nil {
			return nil, err
		}
		if found {
			raised = append(raised, Raised{Marked: m, Intent: waiting})
			continue
		}
		taken, err := c.intake.TakeIn(ctx, Actor, intent.SourceDetector, statement)
		if err != nil {
			return nil, err
		}
		raised = append(raised, Raised{Marked: m, Intent: taken, New: true})
	}
	return raised, nil
}
