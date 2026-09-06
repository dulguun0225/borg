package healthmonitor

import (
	"context"
	"sort"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/window"
)

// policyWindow is the parameters in force for one service, which package policy
// resolves. It is named here so that the resolution appears once in this
// package's signatures.
type policyWindow = policy.Window

// previousRead is the newest closed window of the service that carries a read,
// and false where the service has none. It is what the two questions below are
// answered from: what the traffic reaches, and which operations it reaches it
// on. Traffic changes, so the newest reading is the honest estimate of what the
// next window will get.
func (h *HealthMonitor) previousRead(ctx context.Context, serviceID string) (window.Window, error) {
	all, err := window.All(ctx, h.pool, serviceID)
	if err != nil {
		return window.Window{}, err
	}
	for i := len(all) - 1; i >= 0; i-- {
		if !all[i].Open() && !all[i].ClosedOn.Empty() {
			return all[i], nil
		}
	}
	return window.Window{}, nil
}

// passedReachable is whether the traffic can support the power in force at the
// size in force inside the cap. Where it cannot, passed is not an exit available
// to that window, which runs to the cap the way a first release's does.
//
// It is read off what the service's last window actually reached, which is the
// same arithmetic the score's size in force is computed by: the finest size that
// window's traffic could rule out, read at the power this window asks for, against
// the size this one is asking for. The power is in it because a finest size is
// recorded at the power that window was reading at, and the same traffic asked to
// catch a regression more reliably catches only a coarser one — so a service that
// reached its size at even odds can be refused the exit at a power of nine in ten
// without the traffic having changed at all.
//
// A service with no closed window yet has nothing to be refused on, and the
// question is answered again at every open.
func passedReachable(previous window.Window, o window.OpenEvent) bool {
	if len(previous.FinestSizeReached) == 0 {
		return true
	}
	for quantity, size := range o.Size {
		reached, read := previous.FinestSizeReached[quantity]
		if !read {
			continue
		}
		recordedAt, atAPower := previous.Power[quantity]
		asked, asksAPower := o.Power[quantity]
		if atAPower && asksAPower {
			b, found := previous.Boundary(quantity)
			if !found {
				continue
			}
			atThisPower, err := b.AtPower(reached, recordedAt, asked)
			if err != nil {
				// A power the arithmetic cannot reach is one no count of intervals
				// reaches, which is the window with no passed exit available to it.
				return false
			}
			reached = atThisPower
		}
		if reached > size {
			return false
		}
	}
	return true
}

// operationsReadAlone is the operations whose volume can support the power in
// force at the size in force inside the cap, out of those the service's last
// window read. The rest are pooled into one series per quantity per target and
// read as one.
//
// It is arithmetic over the traffic each operation received in that service's
// past windows, against the units the size in force needs at the rate that
// window's other arm ran at — the within-interval bound, which is the one a
// count of units answers. An operation the service has never read alone before
// is not read alone now: the set is what the evidence supports, and a service
// with no closed window reads every operation pooled until one closes.
func operationsReadAlone(previous window.Window, o window.OpenEvent) []string {
	if previous.ClosedOn.Empty() || len(o.Size) == 0 {
		return nil
	}
	baselineRate := rateOf(previous.ClosedOn.Of(gatepolicy.QuantityErrorRate))
	if baselineRate <= 0 || baselineRate >= 1 {
		return nil
	}

	// The allocation the operations are tested at is the widest one they could
	// produce: every operation the last window read, on every target, on every
	// quantity. Tested at a narrower one, an operation admitted here would widen
	// the set and change the boundary that admitted it.
	perOperation := map[string]int64{}
	for _, series := range previous.ClosedOn.Series {
		if series.Operation == PooledOperation {
			continue
		}
		perOperation[series.Operation] += series.Counts.Units
	}
	if len(perOperation) == 0 {
		return nil
	}
	comparisons := max(len(o.Targets), 1) * (len(perOperation) + 1) * len(o.Size)

	var alone []string
	for operation, units := range perOperation {
		if supportsEveryQuantity(o, comparisons, baselineRate, units) {
			alone = append(alone, operation)
		}
	}
	sort.Strings(alone)
	return alone
}

// supportsEveryQuantity is whether one operation's volume reaches the units
// every quantity's size needs at the power in force. Every, because the
// operation is one series read on all of them: admitting it for one quantity and
// not another would be two sets of operations on one window.
//
// The units bound is the within-interval one and carries no power of its own, so
// the power enters it the way it enters the size: the size the operation has to
// support is read at the power in force through [boundary.Boundary.AtPower],
// from the size asked for at even odds — the power at which the units bound is
// stated. An operation admitted without that reading would be one read alone at
// a size its traffic supports only half the time.
func supportsEveryQuantity(o window.OpenEvent, comparisons int, baselineRate float64, units int64) bool {
	for quantity, size := range o.Size {
		b := boundary.Boundary{
			Size: size, Confidence: o.Confidence, Comparisons: comparisons, Worse: window.Worse(quantity),
		}
		if power, asksAPower := o.Power[quantity]; asksAPower {
			atThisPower, err := b.AtPower(size, evenOdds, power)
			if err != nil {
				return false
			}
			b.Size = atThisPower
		}
		needed, err := b.UnitsToPassed(baselineRate)
		if err != nil || float64(units) < needed {
			return false
		}
	}
	return true
}

// evenOdds is the power the units bound is stated at: it asks how many units a
// release running at the baseline needs before the two rates could be told apart
// at all, which is the expected crossing and not one reached with a stated
// probability. Reading a size from it at the power in force is what puts the two
// bounds on one footing.
const evenOdds = 0.5

// rateOf is one arm's share out of the counts, and nothing where that arm
// counted no units.
func rateOf(counts boundary.Counts) float64 {
	rate, read := counts.BaselineRate()
	if !read {
		return 0
	}
	return rate
}
