package gatepolicy

import (
	"errors"
	"fmt"
	"slices"
)

// Strategy is how a release takes live traffic from the build it replaces. It is
// here because an owner authors a default on production's environment record and
// a safeguard binds it, which is what this package is the vocabulary of; the
// pick a gate makes at the production deploy row is package gate's own, and the
// two spell the names the same way.
type Strategy string

const (
	// StrategyWithControl keeps the build being replaced serving the rest of the
	// traffic throughout, on a schedule.
	StrategyWithControl Strategy = "with a control"
	// StrategyWithoutControl takes all of the traffic, in place, with none of the
	// build it replaces left running.
	StrategyWithoutControl Strategy = "without a control"
)

// Strategies is both rows.
var Strategies = []Strategy{StrategyWithControl, StrategyWithoutControl}

// ErrStrategyUnknown is returned by [DecidableStrategy] for a name outside
// [Strategies].
var ErrStrategyUnknown = errors.New("gatepolicy: that is not a rollout strategy")

// DecidableStrategy is the strategy of that name, and an error for a name
// outside [Strategies]. A caller that took the name from an owner's input calls
// this rather than casting, so a default authored on an environment cannot name
// a strategy no deployer performs.
func DecidableStrategy(name string) (Strategy, error) {
	strategy := Strategy(name)
	if !slices.Contains(Strategies, strategy) {
		return "", fmt.Errorf("%w: %q", ErrStrategyUnknown, name)
	}
	return strategy, nil
}
