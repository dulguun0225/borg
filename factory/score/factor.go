package score

import (
	"context"
	"fmt"
	"strings"
)

// Group is which of the design's three groups a factor belongs to. A human
// arguing with the number argues with a group at a time, which is what the
// grouping is for.
type Group string

const (
	// GroupChange is computed from the change itself.
	GroupChange Group = "change"
	// GroupAuthor is the prior on whoever authored the change.
	GroupAuthor Group = "author"
	// GroupContext is what the change touches around it.
	GroupContext Group = "context"
)

// Half is which half of the score a factor feeds. Likelihood and impact stay
// separate until the last step, because they answer different questions and
// call for different responses: likely-wrong but cheap to undo should ship and
// let a rollback handle it, and unlikely but catastrophic should be gated
// whatever the likelihood. Reversibility is neither half — it discounts impact,
// and the rollout strategy reads it against impact directly.
type Half string

const (
	// HalfLikelihood is how likely the change is to be wrong.
	HalfLikelihood Half = "likelihood"
	// HalfImpact is how much it matters if it is.
	HalfImpact Half = "impact"
	// HalfReversibility is how cheaply it can be undone.
	HalfReversibility Half = "reversibility"
)

// Factor is one named factor of the vector as it is written onto a decision.
// The reading is the quantity the level was resolved from, in words, because a
// level on its own is a number a human cannot argue with. The JSON tags are the
// field names the open event stores.
type Factor struct {
	Name        string  `json:"name"`
	Group       Group   `json:"group"`
	Half        Half    `json:"half"`
	Reading     string  `json:"reading"`
	Level       float64 `json:"level"`
	Weight      float64 `json:"weight"`
	Unavailable string  `json:"unavailable"`
}

// definition is one factor's fixed part: where it sits, what it is worth, and
// what reads it. The levels are computed per change; this is what the score
// version publishes as the factor set.
//
// The reader is a field of the table rather than a name looked up somewhere
// else, so a factor cannot be published without something computing it and the
// compiler is what says so.
type definition struct {
	name   string
	group  Group
	half   Half
	weight float64
	// reads is what the factor is computed from, in the words the factor set
	// the version publishes uses and a human reading the vector sees.
	reads string
	read  func(*Score, context.Context, Change) (reading, error)
}

// definitions is the factor set: the design's three groups, with the change
// group's five factors, the per-author prior, and the context group's two. The
// weights are the authored formula's and sum to one within each half.
var definitions = []definition{
	{"change.size", GroupChange, HalfLikelihood, 0.30,
		"lines the build's diff against master changes", (*Score).size},
	{"change.area_churn", GroupChange, HalfLikelihood, 0.20,
		"releases minted in this item's area lately", (*Score).churn},
	{"change.test_coverage", GroupChange, HalfLikelihood, 0.20,
		"criteria in force that decided this build, and whether any failed", (*Score).coverage},
	{"author.prior", GroupAuthor, HalfLikelihood, 0.30,
		"every outcome on this author's own work: human verdicts on its versions, the analysis windows of its releases, and any of them a human undid", (*Score).prior},
	{"change.reach", GroupChange, HalfImpact, 0.50,
		"share of the service's files the diff touches", (*Score).reach},
	{"context.business_area", GroupContext, HalfImpact, 0.30,
		"human verdicts on items in this area", (*Score).businessArea},
	{"context.consumers", GroupContext, HalfImpact, 0.20,
		"sibling services declaring they consume what this one publishes", (*Score).consumers},
	{"change.reversibility", GroupChange, HalfReversibility, 1.00,
		"whether the service has a release to return to", (*Score).reversibility},
}

// FactorSet is the factor set the score version publishes: every factor, its
// group, its half, its weight, and what it is computed from. It is text because
// a version names what a human reads.
func FactorSet() string {
	var b strings.Builder
	for _, d := range definitions {
		fmt.Fprintf(&b, "%s (%s, %s, weight %.2f): %s\n", d.name, d.group, d.half, d.weight, d.reads)
	}
	return b.String()
}

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
	sizeBreakpoints      = []breakpoint{{50, 0.1}, {200, 0.3}, {600, 0.6}}
	reachBreakpoints     = []breakpoint{{0.1, 0.1}, {0.3, 0.3}, {0.6, 0.6}}
	churnBreakpoints     = []breakpoint{{0, 0.0}, {2, 0.2}, {9, 0.5}}
	criteriaBreakpoints  = []breakpoint{{0, 1.0}, {2, 0.5}, {9, 0.3}}
	consumersBreakpoints = []breakpoint{{0, 0.0}, {2, 0.4}, {9, 0.7}}
)

// evidenceLevel is how a factor computed from outcomes resolves: one minus the
// share of that evidence which was good, with one added to the
// denominator so that no evidence is the top of the scale and a rejection
// counts against the author rather than merely failing to count for them. It is
// what makes an unseen author and an unseen area start wide and narrow with
// evidence, which doc.go separates from a factor being unavailable.
func evidenceLevel(good, bad int) float64 {
	return 1 - float64(good)/float64(good+bad+1)
}
