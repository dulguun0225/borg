package service

import "github.com/dulguun0225/borg/factory/gatepolicy"

// ShippedWindowLimit is the window limit a service starts at: one, the value
// the score supplies where an owner authored none. The column is left null at
// creation, the way the four fixed defaults of shipped.go are, so that an
// unauthored limit stays the score's to move.
const ShippedWindowLimit = 1

// WindowLimitInForce is the window limit a reader outside gate policy's
// resolution takes: what an owner authored, or [ShippedWindowLimit] where
// nothing is. A caller reads this rather than the raw field so a row with
// nothing authored is read the one way.
func WindowLimitInForce(a gatepolicy.Authored) float64 { return a.Or(ShippedWindowLimit) }
