package contractcheck

import (
	"context"
	"fmt"
	"slices"

	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/release"
)

// What a candidate's environment is composed from. A candidate's environment
// holds the producers the candidate build's consumer contract names, and theirs
// through their current releases' consumer contracts — so the composition is a
// walk over the one field that holds the edge between two services, and not over
// anything an item declares.
//
// A candidate's own consumer contract exists before this is asked: it is an
// artifact of the item, derived at the implementation stage from that item's
// build, which is above the row that composes the environment. A producer's is
// the one its current release's item derived.
//
// An absence is not read as no edge: a call site the extractor could not trace
// to an entry makes the whole record could not derive, and a consumer with a
// could-not-derive contract is in no candidate environment's composition, the
// human at the gate being what decides it.

// Composed is one producer a candidate's environment is composed from: the
// service, the release of it that is current in production, and the entries of a
// consumer contract that reach it.
type Composed struct {
	ServiceID string
	// ReleaseID is the producer's current release, and empty where the producer
	// is running nothing — an entry there is nothing to put in place for, which
	// the caller reports rather than composing a hole.
	ReleaseID string
	// Addresses is the entries of the configuration file whose predicates name
	// this producer, in the order they were derived. They are what an address is
	// written for per environment.
	Addresses []string
	// Through is empty for a producer the candidate's own build names, and is
	// the service whose current release's consumer contract named it otherwise.
	Through string
}

// ComposedFrom is the producers a candidate's environment is composed from: the
// producers the candidate build's consumer contract names, and theirs through
// their current releases' consumer contracts, breadth first and in service
// order so that two runs over one graph compose the same list.
//
// The candidate's own service is not among them whatever it declares: a service
// declares against its own store contract exactly as against another service's
// interface, and its own store is not a producer to put in place beside it.
func (c *Check) ComposedFrom(ctx context.Context, itemID, serviceID, production string) ([]Composed, error) {
	for _, required := range []struct{ what, value string }{
		{"item", itemID}, {"service", serviceID}, {"production environment", production},
	} {
		if required.value == "" {
			return nil, fmt.Errorf("%w: the composition names no %s", ErrCandidateIncomplete, required.what)
		}
	}
	prodEnv, err := environment.Get(ctx, c.pool, production)
	if err != nil {
		return nil, err
	}
	addresses := prodEnv.Addresses()

	declared, err := consumercontract.ForItems(ctx, c.pool, []string{itemID})
	if err != nil {
		return nil, err
	}

	var composed []Composed
	reached := map[string]bool{serviceID: true}
	frontier := []reaching{{predicates: declared}}
	for len(frontier) > 0 {
		var next []reaching
		for _, one := range frontier {
			for _, producer := range producersOf(one.predicates, reached) {
				reached[producer.ServiceID] = true
				producer.Through = one.through
				current, running, err := deploy.Current(ctx, c.pool, producer.ServiceID, production, addresses)
				if err != nil {
					return nil, err
				}
				if running {
					producer.ReleaseID = current.ReleaseID
				}
				composed = append(composed, producer)
				if !running {
					// Nothing of that producer is running, so there is no
					// release whose consumer contract could name a producer of
					// its own.
					continue
				}
				rel, err := release.Get(ctx, c.pool, current.ReleaseID)
				if err != nil {
					return nil, err
				}
				if rel.ItemID == "" {
					// A release minted over an accepted commit names no item, so
					// no consumer contract was derived for it.
					continue
				}
				theirs, err := consumercontract.ForItems(ctx, c.pool, []string{rel.ItemID})
				if err != nil {
					return nil, err
				}
				next = append(next, reaching{through: producer.ServiceID, predicates: theirs})
			}
		}
		frontier = next
	}
	return composed, nil
}

// reaching is one layer of the walk: the predicates of one consumer contract,
// and the service whose contract they are.
type reaching struct {
	through    string
	predicates []consumercontract.Predicate
}

// producersOf is the producers these predicates name that nothing has reached
// yet, in service order, each with the addresses that reach it. A predicate
// whose producer resolved to no service record names a service the factory has
// never seen publish anything, and there is nothing to put in place for it.
func producersOf(predicates []consumercontract.Predicate, reached map[string]bool) []Composed {
	byService := map[string]*Composed{}
	var ids []string
	for _, p := range predicates {
		if p.ProducerServiceID == "" || reached[p.ProducerServiceID] {
			continue
		}
		one, held := byService[p.ProducerServiceID]
		if !held {
			one = &Composed{ServiceID: p.ProducerServiceID}
			byService[p.ProducerServiceID] = one
			ids = append(ids, p.ProducerServiceID)
		}
		if !slices.Contains(one.Addresses, p.Address) {
			one.Addresses = append(one.Addresses, p.Address)
		}
	}
	slices.Sort(ids)
	producers := make([]Composed, 0, len(ids))
	for _, id := range ids {
		producers = append(producers, *byService[id])
	}
	return producers
}
