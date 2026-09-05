package score

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// FactorSet is which set of factors a firing is scored on. One threshold is read
// against three sets, so the number every set returns is on one scale and each
// set's weights are fitted apart on the held-out decisions taken on that set
// alone.
//
// Which set a firing is on is the caller's, because a gate row is package gate's
// vocabulary and not this package's. A change naming none is refused rather than
// defaulted: a set chosen by inference is a decision nobody made.
type FactorSet string

const (
	// SetWithABuild is the four rows below a build: the change, the author, the
	// exposure and the context groups, exposure being read from a diff and a
	// build record.
	SetWithABuild FactorSet = "with a build"
	// SetAboveABuild is Decomposition, Spec, Implementation plan and Tasks. The
	// exposure group is inapplicable there and not unavailable — no vector
	// records it, and nothing is resolved on its account.
	SetAboveABuild FactorSet = "above a build"
	// SetRolePromptOrSkill is the row that decides a version of what an agent is
	// told. The change group is replaced by three factors of its own, a role
	// prompt having no code to have sized and no area to have churned; the
	// author and context groups are unchanged.
	SetRolePromptOrSkill FactorSet = "a role prompt or a skill"
)

// FactorSets is every set, in the order a version publishes them.
var FactorSets = []FactorSet{SetWithABuild, SetAboveABuild, SetRolePromptOrSkill}

// ErrFactorSetUnknown is returned by [Score.Assess] for a change naming a set
// outside [FactorSets].
var ErrFactorSetUnknown = errors.New("score: the change names no factor set of the three")

// definitionsOf is the factors one set weighs, in vector order.
func definitionsOf(set FactorSet) []definition {
	switch set {
	case SetWithABuild:
		return []definition{
			changeSize, changeAreaChurn, changeReach, changeReversibility,
			authorPrior, exposureReach,
			contextHazardSeverity, contextIntentSource, contextConsumers,
		}
	case SetAboveABuild:
		return []definition{
			changeSize, changeAreaChurn, changeReach, changeReversibility,
			authorPrior,
			contextHazardSeverity, contextIntentSource, contextConsumers,
		}
	case SetRolePromptOrSkill:
		return []definition{
			fleetShare, fleetDeparture, fleetReversibility,
			authorPrior,
			contextHazardSeverity, contextIntentSource, contextConsumers,
		}
	}
	return nil
}

// Weights is what one factor set gives each of its factors, by factor name. It
// is a field of the [Version] from the first row and not a fact kept beside it:
// a decision re-scored under the version it names has to return the number it
// was decided on, and a formula recorded without its weights is a shape and not
// a function.
type Weights map[string]float64

// Of is the weight this set gives one factor, and nothing for a factor the set
// does not weigh.
func (w Weights) Of(factor string) float64 { return w[factor] }

// Text renders one set's weights in factor order, which is what a reader
// comparing two versions reads.
func (w Weights) Text() string {
	names := make([]string, 0, len(w))
	for name := range w {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "%s %.2f ", name, w[name])
	}
	return strings.TrimSpace(b.String())
}

// shipped is the weights the product ships for each set, which a set whose
// held-out decisions are too few to fit keeps. They are the authored formula's,
// calibrated against a factory that has just been installed, and they sum to one
// within each term of each set.
var shipped = map[FactorSet]Weights{
	SetWithABuild: {
		"change.size": 0.25, "change.area_churn": 0.20, "author.prior": 0.35,
		"context.intent_source": 0.20,
		"change.reach":          0.35, "exposure.reach": 0.30,
		"context.hazard_severity": 0.20, "context.consumers": 0.15,
		"change.reversibility": 1.00,
	},
	SetAboveABuild: {
		"change.size": 0.30, "change.area_churn": 0.20, "author.prior": 0.30,
		"context.intent_source":   0.20,
		"change.reach":            0.50,
		"context.hazard_severity": 0.30, "context.consumers": 0.20,
		"change.reversibility": 1.00,
	},
	SetRolePromptOrSkill: {
		"fleet.departure": 0.40, "author.prior": 0.40, "context.intent_source": 0.20,
		"fleet.share_working_from_it": 0.55,
		"context.hazard_severity":     0.30, "context.consumers": 0.15,
		"fleet.reversibility": 1.00,
	},
}

// ShippedWeights is the weights the product ships for one set. A recalibration
// that has too few held-out decisions to fit a set falls back to these, and the
// count behind that fallback is published on the version.
func ShippedWeights(set FactorSet) Weights {
	out := Weights{}
	for name, weight := range shipped[set] {
		out[name] = weight
	}
	return out
}

// ShippedWeightsBySet is the whole table a version starts from.
func ShippedWeightsBySet() map[FactorSet]Weights {
	out := map[FactorSet]Weights{}
	for _, set := range FactorSets {
		out[set] = ShippedWeights(set)
	}
	return out
}

// FactorSetsText is what a version publishes as its factor sets: every set,
// every factor in it, and the weight that set gives each. It is text because a
// version names what a human reads, and the weights it names are the version's
// own, so a recalibrated version publishes the weights it was recalibrated to.
func FactorSetsText(weights map[FactorSet]Weights) string {
	var b strings.Builder
	for _, set := range FactorSets {
		b.WriteString(factorSetText(set, weights[set]))
	}
	return b.String()
}
