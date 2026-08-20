package contractcheck

import (
	"context"
	"slices"

	"github.com/dulguun0225/borg/factory/declaration"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/window"
)

// InForce is which declarations of one service bind at this moment, and the range
// they were derived over: the service's restore floor up to its newest release.
//
// The range is on the value because a reader of a declaration has to be able to
// see what made it binding. HasFloor false is a service none of whose windows has
// closed clean or at the cap, and the range is then every release it has — the
// direction a first release's missing rollback target already takes.
type InForce struct {
	ServiceID string
	// FloorNumber is the release the restore floor is, and HasFloor is whether
	// the service has one at all.
	FloorNumber int64
	HasFloor    bool
	// FloorWindowID is the window whose close set the floor, and is empty where
	// there is no floor.
	FloorWindowID string
	// HighestNumber is the service's newest release, and 0 where it has none.
	HighestNumber int64
	// ItemIDs is the items of the releases in the range, which is the set the
	// predicates were derived by.
	ItemIDs []string
	// Predicates is every predicate those items declared, whichever producer.
	Predicates []declaration.Predicate
}

// RestoreFloor is the release a service could return to at all: the release
// watched by the newest closed window whose exit is clean or at the cap. false is
// a service with none, which is every service until one of its windows has closed
// that way.
//
// It is computed here and not in package comparison, which computes a rollback's
// target from the same windows. The two coincide whenever windows close in the
// order they opened and differ exactly where they do not, and they answer different
// questions: a target is computed for one rollback and is the newest release below
// the release being rolled back, where the floor is a standing value per service.
// Neither is computed in package window, which cannot read a release's number.
//
// Newest by the time the window closed and not by the release's number, which is
// what "the newest closed window" says. Those differ exactly where windows close
// out of order, and what the floor is about is which close is the most recent
// evidence.
func (c *Check) RestoreFloor(ctx context.Context, serviceID string) (release.Release, string, bool, error) {
	closed, err := window.ClosedWithoutHarm(ctx, c.pool, serviceID)
	if err != nil {
		return release.Release{}, "", false, err
	}
	if len(closed) == 0 {
		return release.Release{}, "", false, nil
	}
	// ClosedWithoutHarm returns them newest close first, which is the order this
	// question is asked in and the one thing this reads it for.
	newest := closed[0]
	floor, err := release.Get(ctx, c.pool, newest.ReleaseID)
	if err != nil {
		return release.Release{}, "", false, err
	}
	return floor, newest.ID, true, nil
}

// DeclarationsInForce is which declarations of one service bind at this moment.
// One range answers three questions at once — what runs now, what has merged and
// will run, and what a rollback can still restore — and it is the same query for
// an interface and for a store.
//
// What it costs is the design's own: the range is as wide as the windows a service
// has open and wider wherever a release has merged and not deployed, so a producer
// is held to the assumptions of releases that are not running. That is the
// direction that keeps a promise to a release a rollback could bring back.
func (c *Check) DeclarationsInForce(ctx context.Context, serviceID string) (InForce, error) {
	in := InForce{ServiceID: serviceID}
	if serviceID == "" {
		return in, nil
	}
	highest, found, err := release.Highest(ctx, c.pool, serviceID)
	if err != nil {
		return InForce{}, err
	}
	if !found {
		// A service with no release has nothing in force: every declaration it has
		// derived belongs to a candidate that has not merged, and what filters
		// those is there being no release naming their item.
		return in, nil
	}
	in.HighestNumber = highest.Number

	lowest := int64(1)
	floor, windowID, hasFloor, err := c.RestoreFloor(ctx, serviceID)
	if err != nil {
		return InForce{}, err
	}
	if hasFloor {
		in.HasFloor, in.FloorNumber, in.FloorWindowID = true, floor.Number, windowID
		lowest = floor.Number
	}

	releases, err := release.Between(ctx, c.pool, serviceID, lowest, highest.Number)
	if err != nil {
		return InForce{}, err
	}
	for _, r := range releases {
		if !slices.Contains(in.ItemIDs, r.ItemID) {
			in.ItemIDs = append(in.ItemIDs, r.ItemID)
		}
	}
	in.Predicates, err = declaration.ForItems(ctx, c.pool, in.ItemIDs)
	if err != nil {
		return InForce{}, err
	}
	return in, nil
}

// Binding is every predicate in force anywhere in the factory that names one
// producer, with the range each consumer's own declarations were derived over. It
// is the read both the producer's merge row and the deprecation list are a use of:
// one asks what the candidate has to satisfy, the other what still names one
// element.
//
// Which services to ask is read off the declarations themselves rather than off
// every service there is: what makes a service a candidate consumer is a
// declaration naming this producer, which is a row of that table. A service on that
// list may have nothing in force, its declarations all falling outside its own
// range, which is what the range is for.
//
// The producer itself is among them, and that is not an accident: a service
// declares against its own store contract exactly as against another service's
// interface, and that consumer — the service's own past — is why a store promises
// forward.
func (c *Check) Binding(ctx context.Context, producerServiceID string) ([]declaration.Predicate, []InForce, error) {
	consumers, err := declaration.ConsumerServicesEver(ctx, c.pool, producerServiceID)
	if err != nil {
		return nil, nil, err
	}
	var binding []declaration.Predicate
	ranges := make([]InForce, 0, len(consumers))
	for _, consumer := range consumers {
		in, err := c.DeclarationsInForce(ctx, consumer)
		if err != nil {
			return nil, nil, err
		}
		ranges = append(ranges, in)
		for _, p := range in.Predicates {
			if p.ProducerServiceID == producerServiceID {
				binding = append(binding, p)
			}
		}
	}
	return binding, ranges, nil
}
