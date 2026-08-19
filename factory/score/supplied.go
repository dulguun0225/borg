package score

import (
	"fmt"
	"strings"

	"github.com/dulguun0225/borg/factory/gatepolicy"
)

// supply is one value the score supplies where an owner authored nothing, and
// why that value. The reason is published with the number, because a default
// nobody chose is still a decision and it can stay invisible until it takes
// effect.
type supply struct {
	parameter gatepolicy.Parameter
	value     float64
	why       string
}

// supplies is the value the score supplies for six of gate policy's seven rows.
// There is none for the predicate catalog, which no outcome teaches, so a
// factory with nothing authored has an empty catalog and not a supplied one.
//
// These are authored numbers on an authored formula. Six of them move as
// outcomes arrive in the design, and none of them moves here.
var supplies = []supply{
	{
		gatepolicy.RiskThreshold, 0.30,
		"calibrated so that a service's first release — no earlier release to return to, an author nobody has approved, an area with no history — is decided by a human, and the item after it is not",
	},
	{
		gatepolicy.AttemptBound, 3,
		"a stage that fails once has usually had a reply the protocol refused rather than work the factory cannot do, and a bound this low turns solvable work into human work no more than a few tokens later",
	},
	{
		gatepolicy.ItemSizeTarget, 300,
		"lines, above the minimum that an item ships by itself; nothing reads it until a cut sizes anything",
	},
	{
		gatepolicy.WindowSize, 0.02,
		"the smallest regression a comparison must rule out, as a share; the traffic a comparison needs scales as the inverse square of this, so it is the coarse end of what is worth catching",
	},
	{
		gatepolicy.WindowConfidence, 0.95,
		"the confidence required of that comparison, at the convention a reader of a sequential test expects",
	},
	{
		gatepolicy.WindowCap, 86400,
		"seconds — a day, after which a window that will never reach its volume ends unresolved rather than holding the next deploy indefinitely",
	},
	{
		gatepolicy.K, 1,
		"the serial factory: one window open per service, so a rollback undoes one release, which is the safe end of a parameter whose cost appears only at the first rollback",
	},
}

// Supplied is the value the score supplies for one parameter, and false for one
// it supplies none for. The predicate catalog is the only one it supplies none
// for, and a caller reading false there has an empty catalog rather than a
// missing value.
func Supplied(p gatepolicy.Parameter) (float64, bool) {
	for _, s := range supplies {
		if s.parameter == p {
			return s.value, true
		}
	}
	return 0, false
}

// SuppliedText is what the score version stores: every value the score
// supplies, with the reason for each. A version differs from its predecessor
// where this text does, so changing a supplied value appends a version.
func SuppliedText() string {
	var b strings.Builder
	for _, s := range supplies {
		fmt.Fprintf(&b, "%s = %v: %s\n", s.parameter, s.value, s.why)
	}
	return b.String()
}
