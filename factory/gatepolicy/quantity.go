package gatepolicy

import (
	"errors"
	"fmt"
	"slices"
)

// Quantity is one of the numbers the health monitor reads. The analysis window's
// size and its power are authored per quantity as well as per service, because a
// detectable change in an error rate and one in a latency quantile are not one
// number.
//
// They are vocabulary and not a table, which is why they are here: the window
// records what it was read at, the health monitor reads them, and package policy
// resolves a size and a power per one — and none of the three should hold a
// second copy of the list.
type Quantity string

const (
	// QuantityRequestRate is the service's request rate, and the one quantity the
	// comparison also reads from the outside, because it is the one a release can
	// move by emitting nothing.
	QuantityRequestRate Quantity = "request_rate"
	// QuantityErrorRate is the service's error rate. Availability is this read
	// over a period rather than a quantity of its own.
	QuantityErrorRate Quantity = "error_rate"
	// QuantityLatency is the latency quantile fixed with the histogram's bucket
	// boundaries, read at the resolution those buckets give and no finer.
	QuantityLatency Quantity = "latency"
	// QuantityHazardousOperation is the count of times the software performed the
	// operation an irreversible area names. The health monitor reads it as a fourth
	// quantity for a service whose area is graded irreversible and for no other.
	QuantityHazardousOperation Quantity = "hazardous_operation"
)

// Quantities is every quantity the health monitor reads: the three every service
// emits, and the fourth kept for a service whose area is graded irreversible.
var Quantities = []Quantity{
	QuantityRequestRate, QuantityErrorRate, QuantityLatency, QuantityHazardousOperation,
}

// ErrQuantityUnknown is returned by [DecidableQuantity] for a name outside
// [Quantities]. Saturation is the case the design names: it is read by its effect
// and never modelled, so it arrives here as a name nothing reads.
var ErrQuantityUnknown = errors.New("gatepolicy: the health monitor reads no such quantity")

// DecidableQuantity is the quantity by that name, and an error for a name outside
// [Quantities]. A caller that took the name from an owner's input calls this
// rather than casting, so a size authored per quantity cannot name one nothing
// measures.
func DecidableQuantity(name string) (Quantity, error) {
	quantity := Quantity(name)
	if !slices.Contains(Quantities, quantity) {
		return "", fmt.Errorf("%w: %q", ErrQuantityUnknown, name)
	}
	return quantity, nil
}
