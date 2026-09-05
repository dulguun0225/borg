package contractcheck

import (
	"context"
	"slices"

	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/window"
)

// InForce is which consumer contracts of one service bind at this moment, and the
// range they were derived over: the service's last known-good release up to its
// newest release.
//
// The range is on the value because a reader of a consumer contract has to be
// able to see what made it binding. HasLastKnownGood false is a service none of
// whose windows has closed passed or timed out, and the range is then every
// release it has — the direction a first release's missing rollback target
// already takes.
type InForce struct {
	ServiceID string
	// LastKnownGoodNumber is which release the last known-good release is, and
	// HasLastKnownGood is whether the service has one at all.
	LastKnownGoodNumber int64
	HasLastKnownGood    bool
	// LastKnownGoodWindowID is the window whose close set it, and is empty where
	// there is none.
	LastKnownGoodWindowID string
	// HighestNumber is the service's newest release, and 0 where it has none.
	HighestNumber int64
	// ItemIDs is the items of the releases in the range, which is the set the
	// predicates were derived by.
	ItemIDs []string
	// Predicates is every predicate those items declared, whichever producer.
	Predicates []consumercontract.Predicate
}

// LastKnownGood is the release a service could return to at all: the release
// watched by the newest closed window whose exit is passed or timed out. false is
// a service with none, which is every service until one of its windows has closed
// that way.
//
// It is computed here and not in package healthmonitor, which computes a rollback's
// target from the same windows. The two coincide whenever windows close in the
// order they opened and differ exactly where they do not, and they answer different
// questions: a target is computed for one rollback and is the newest release below
// the release being rolled back, where the last known-good release is a standing
// value per service. Neither is computed in package window, which cannot read a
// release's number.
//
// Newest by the time the window closed and not by the release's number, which is
// what "the newest closed window" says. Those differ exactly where windows close
// out of order, and what the last known-good release is about is which close is
// the most recent evidence.
func (c *Check) LastKnownGood(ctx context.Context, serviceID string) (release.Release, string, bool, error) {
	closed, err := window.ClosedPassedOrTimedOut(ctx, c.pool, serviceID)
	if err != nil {
		return release.Release{}, "", false, err
	}
	if len(closed) == 0 {
		return release.Release{}, "", false, nil
	}
	// ClosedPassedOrTimedOut returns them newest close first, which is the order
	// this question is asked in and the one thing this reads it for.
	newest := closed[0]
	lastGood, err := release.Get(ctx, c.pool, newest.ReleaseID)
	if err != nil {
		return release.Release{}, "", false, err
	}
	return lastGood, newest.ID, true, nil
}

// ConsumerContractsInForce is which consumer contracts of one service bind at
// this moment. One range answers three questions at once — what runs now, what
// has merged and will run, and what a rollback can still restore — and it is the
// same query for an interface and for a store.
//
// What it costs is the design's own: the range is as wide as the windows a service
// has open and wider wherever a release has merged and not deployed, so a producer
// is held to the assumptions of releases that are not running. That is the
// direction that keeps a promise to a release a rollback could bring back.
func (c *Check) ConsumerContractsInForce(ctx context.Context, serviceID string) (InForce, error) {
	in := InForce{ServiceID: serviceID}
	if serviceID == "" {
		return in, nil
	}
	highest, found, err := release.Highest(ctx, c.pool, serviceID)
	if err != nil {
		return InForce{}, err
	}
	if !found {
		// A service with no release has nothing in force: every consumer contract it
		// has derived belongs to a candidate that has not merged, and what filters
		// those is there being no release naming their item.
		return in, nil
	}
	in.HighestNumber = highest.Number

	lowest := int64(1)
	lastGood, windowID, has, err := c.LastKnownGood(ctx, serviceID)
	if err != nil {
		return InForce{}, err
	}
	if has {
		in.HasLastKnownGood, in.LastKnownGoodNumber, in.LastKnownGoodWindowID = true, lastGood.Number, windowID
		lowest = lastGood.Number
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
	in.Predicates, err = consumercontract.ForItems(ctx, c.pool, in.ItemIDs)
	if err != nil {
		return InForce{}, err
	}
	return in, nil
}

// Binding is every predicate in force anywhere in the factory that names one
// producer, with the range each consumer's own consumer contracts were derived
// over. It is the read both the producer's merge row and the deprecation list are
// a use of: one asks what the candidate has to satisfy, the other what still names
// one element.
//
// Which services to ask is read off the consumer contracts themselves rather than
// off every service there is: what makes a service a candidate consumer is a
// consumer contract naming this producer, which is a row of that table. A service
// on that list may have nothing in force, its consumer contracts all falling
// outside its own range, which is what the range is for.
//
// The producer itself is among them, and that is not an accident: a service
// declares against its own store contract exactly as against another service's
// interface, and that consumer — the service's own past — is why a store promises
// forward.
func (c *Check) Binding(ctx context.Context, producerServiceID string) ([]consumercontract.Predicate, []InForce, error) {
	consumers, err := consumercontract.ConsumerServicesEver(ctx, c.pool, producerServiceID)
	if err != nil {
		return nil, nil, err
	}
	var binding []consumercontract.Predicate
	ranges := make([]InForce, 0, len(consumers))
	for _, consumer := range consumers {
		in, err := c.ConsumerContractsInForce(ctx, consumer)
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
