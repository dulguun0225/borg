package score

import (
	"context"
	"fmt"
	"strings"
)

// Group is which of the design's four groups a factor belongs to. A human
// arguing with the number argues with a group at a time, which is what the
// grouping is for.
type Group string

const (
	// GroupChange is computed from the change itself.
	GroupChange Group = "change"
	// GroupAuthor is the prior on whoever authored the change.
	GroupAuthor Group = "author"
	// GroupExposure is what the change reaches that the service did not reach
	// before, read from the diff and the build's resolved set. It only ever
	// raises the number and learns from no outcome.
	GroupExposure Group = "exposure"
	// GroupContext is what the change touches in this customer's business.
	GroupContext Group = "context"
)

// Term is which term of the published formula a factor feeds. Likelihood and
// impact stay separate until the last step, because they answer different
// questions and call for different responses: likely-wrong but cheap to undo
// should ship and let a rollback handle it, and unlikely but catastrophic should
// be gated whatever the likelihood. Reversibility is a term of its own — it
// discounts impact, and the rollout strategy reads it against impact directly.
type Term string

const (
	// TermLikelihood is how likely the change is to be wrong.
	TermLikelihood Term = "likelihood"
	// TermImpact is how much it matters if it is.
	TermImpact Term = "impact"
	// TermReversibility is how cheaply it can be undone, which discounts impact.
	TermReversibility Term = "reversibility"
)

// Factor is one named factor of the vector as it is written onto a decision.
// The reading is the quantity the level was resolved from, in words, because a
// level on its own is a number a human cannot argue with. The JSON tags are the
// field names the open event stores.
//
// Resolved is why this factor was resolved rather than weighed, and is empty on
// a factor the formula weighed. A resolved factor is left out of the weighted
// means and puts a human at the gate whatever the number returns, which is
// [Assessment.Resolved] and not a level of its own.
type Factor struct {
	Name     string  `json:"name"`
	Group    Group   `json:"group"`
	Term     Term    `json:"term"`
	Reading  string  `json:"reading"`
	Level    float64 `json:"level"`
	Weight   float64 `json:"weight"`
	Resolved string  `json:"resolved,omitempty"`
	// Evidence is the exposure factor's own list — each call, each credential
	// name, each check, each package with its version and licence, and the file
	// and line of each — which is what a human at Implementation argues with
	// beside the diff. Every other factor carries none.
	Evidence []string `json:"evidence,omitempty"`
	// Width and Closes are the per-author prior's width and the count of
	// resolved window exits behind it, written beside the factor so that a
	// narrow prior earned on outcomes is readable from one that only waited.
	// Every other factor leaves both at nothing.
	Width  float64 `json:"width,omitempty"`
	Closes int     `json:"closes,omitempty"`
	// Claimed and Verified are how many of the rows behind the prior were
	// written by an actor whose key nothing had verified and how many by one
	// seam 5 verified. A claimed row is learned from as a row that says so.
	Claimed  int `json:"claimed,omitempty"`
	Verified int `json:"verified,omitempty"`
}

// definition is one factor's fixed part: where it sits, what it is worth under
// each factor set, and what reads it. The levels are computed per change; this
// is what the score version publishes as the factor set.
//
// The reader is a field of the table rather than a name looked up somewhere
// else, so a factor cannot be published without something computing it and the
// compiler is what says so.
type definition struct {
	name  string
	group Group
	term  Term
	// reads is what the factor is computed from, in the words the factor set
	// the version publishes uses and a human reading the vector sees.
	reads string
	read  func(*Score, context.Context, Change) (reading, error)
}

// The factors of the four groups, named once so that a factor set names a
// factor and not a second copy of it.
var (
	changeSize = definition{"change.size", GroupChange, TermLikelihood,
		"lines the build's diff against master changes, or the requirements the proposed set answers at Decomposition", (*Score).size}
	changeAreaChurn = definition{"change.area_churn", GroupChange, TermLikelihood,
		"releases minted in this item's area lately", (*Score).churn}
	changeReach = definition{"change.reach", GroupChange, TermImpact,
		"share of the service's files the diff touches", (*Score).reach}
	changeReversibility = definition{"change.reversibility", GroupChange, TermReversibility,
		"whether the service has a release to return to, and whether the diff destroys stored data", (*Score).reversibility}
	authorPrior = definition{"author.prior", GroupAuthor, TermLikelihood,
		"every outcome on this author's own work: human verdicts on its versions, and the analysis windows of its releases that closed passed or failed", (*Score).prior}
	exposureReach = definition{"exposure.reach", GroupExposure, TermImpact,
		"what the change reaches that the service did not reach before: an outbound call added, a credential named or read, an authorization check removed or weakened, and a dependency change", (*Score).exposure}
	contextHazardSeverity = definition{"context.hazard_severity", GroupContext, TermImpact,
		"the hazard severity in force on this item's area, which is this group's one declared input", (*Score).hazardSeverity}
	contextIntentSource = definition{"context.intent_source", GroupContext, TermLikelihood,
		"where the intent this item answers came from", (*Score).intentSource}
	contextConsumers = definition{"context.consumers", GroupContext, TermImpact,
		"sibling services declaring they consume what this one publishes", (*Score).consumers}
	contextProtectionWithdrawn = definition{"context.protection_withdrawn", GroupContext, TermImpact,
		"whether the version under decision withdraws a criterion whose provenance names an authority, or admits a transition a human-confirmed screen state machine forbade", (*Score).protectionWithdrawn}
	fleetShare = definition{"fleet.share_working_from_it", GroupChange, TermImpact,
		"the share of the factory working from the version in force this one replaces", (*Score).fleetShare}
	fleetDeparture = definition{"fleet.departure", GroupChange, TermLikelihood,
		"how far this version differs from the version in force", (*Score).fleetDeparture}
	fleetReversibility = definition{"fleet.reversibility", GroupChange, TermReversibility,
		"a version withdrawn is a second record and nothing was deployed, so every version of what an agent is told is reversible", (*Score).fleetReversibility}
)

// level resolves one raw quantity to a level between nothing and one against
// breakpoints, each pair being a ceiling and the level for a quantity at or
// under it, and the last level standing for everything above them all. The
// breakpoints are part of the published formula, so [Formula] states the same
// numbers and TestFormulaStatesEveryBreakpoint holds the two together.
func level(quantity float64, breakpoints []breakpoint, above float64) float64 {
	for _, b := range breakpoints {
		if quantity <= b.upTo {
			return b.level
		}
	}
	return above
}

type breakpoint struct {
	upTo  float64
	level float64
}

// The breakpoints of each factor computed from a quantity. A factor computed
// from a proportion of outcomes has none: its level is arithmetic on the count,
// which [Formula] states.
var (
	sizeBreakpoints         = []breakpoint{{50, 0.1}, {200, 0.3}, {600, 0.6}}
	requirementsBreakpoints = []breakpoint{{2, 0.1}, {5, 0.3}, {12, 0.6}}
	reachBreakpoints        = []breakpoint{{0.1, 0.1}, {0.3, 0.3}, {0.6, 0.6}}
	churnBreakpoints        = []breakpoint{{0, 0.0}, {2, 0.2}, {9, 0.5}}
	consumersBreakpoints    = []breakpoint{{0, 0.0}, {2, 0.4}, {9, 0.7}}
	exposureBreakpoints     = []breakpoint{{0, 0.0}, {2, 0.4}, {5, 0.7}}
)

// evidenceLevel is how a factor computed from outcomes resolves: one minus the
// share of that evidence which was good, with one added to the denominator so
// that no evidence is the top of the scale and a rejection counts against the
// author rather than merely failing to count for them. It is what makes an
// unseen author start wide and narrow with evidence, which doc.go separates
// from a factor being resolved.
func evidenceLevel(good, bad int) float64 {
	return 1 - float64(good)/float64(good+bad+1)
}

// factorSetText renders one factor set for [Version]: every factor, its group,
// its term, the weight this set gives it, and what it is computed from.
func factorSetText(set FactorSet, weights Weights) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", set)
	for _, d := range definitionsOf(set) {
		fmt.Fprintf(&b, "  %s (%s, %s, weight %.2f): %s\n", d.name, d.group, d.term, weights.Of(d.name), d.reads)
	}
	return b.String()
}
