package gate

import "context"

// Score is what the gate asks about a change before it fires: the factor
// vector, the number the published formula reduces it to, and whether a human
// decides. [Stub] is the implementation until M2 authors a real score, and
// the real one replaces it by satisfying this interface.
type Score interface {
	// Assess is the score's answer for one change. The assessment names the
	// score version it was computed under, because the version moves as
	// outcomes arrive and the decision has to name the one in force at the
	// firing.
	Assess(ctx context.Context, c Change) (Assessment, error)
}

// Change is what the score is asked about: the item, the build, and the
// artifact version under decision. Each field is an id; the score reads the
// records they name.
type Change struct {
	ItemID     string
	BuildID    string
	ArtifactID string
}

// Assessment is the score's answer: the vector a human argues with, the
// number a gate compares, the version both were computed under, and whether a
// human decides.
type Assessment struct {
	// Version is the score version the assessment was computed under. The
	// opening row names it, so recomputing the vector later under a moved
	// version cannot pass for the vector the decision was made on.
	Version string
	// Number is what the published formula reduces the vector to. It is text
	// and not a float, because the stub has no formula and answers with a
	// name rather than a quantity; a numeric score is still text a reader
	// parses.
	Number string
	// HumanDecides is whether a human decides at the gate. True puts one
	// there; false is an auto-pass, which nothing in M1 ever answers.
	HumanDecides bool
	// Vector is the named factors the number was reduced from, recorded on
	// the opening row so it exists while a human is deciding.
	Vector []Factor
}

// Factor is one named factor of the vector. A factor the score cannot
// compute resolves to the value that puts a human at the gate, and the vector
// records which factor was unavailable and why rather than leaving a gap a
// reader has to interpret. The JSON tags are the field names the opening
// payload stores.
type Factor struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	// Unavailable is why the score could not compute this factor, and the
	// empty string on a factor it computed.
	Unavailable string `json:"unavailable"`
}

// StubVersion is the score version [Stub] names on every assessment.
const StubVersion = "score-stub-m1"

// StubUnavailable is the reason every factor of the stub's vector carries.
const StubUnavailable = "the score is a stub until M2"

// StubValue is the value every factor of the stub's vector resolves to. A
// factor the score cannot compute resolves to the value that puts a human at
// the gate; the stub can compute none, so every factor resolves the same way,
// and with no published formula there is no scale to place a quantity on.
const StubValue = "human-decides"

// stubFactorNames is one factor per factor the design names, across its three
// groups — the change (size, how much of the system it can affect, area
// churn, test coverage, reversibility), authorship (the author's prior), and
// context (what the change touches in this customer's business, and which
// sibling services consume what it publishes).
var stubFactorNames = []string{
	"change.size",
	"change.reach",
	"change.area_churn",
	"change.test_coverage",
	"change.reversibility",
	"authorship.prior",
	"context.business_area",
	"context.consumers",
}

// Stub is the score until M2 authors one. Its answer is always that a human
// decides: it computes no factor, so every factor of its vector is marked
// unavailable with the reason, and its number is the name of that answer
// rather than a quantity.
type Stub struct{}

// Assess ignores the change and returns the one assessment the stub gives.
// The vector is built per call, so a caller that edits its copy edits nothing
// shared.
func (Stub) Assess(_ context.Context, _ Change) (Assessment, error) {
	vector := make([]Factor, 0, len(stubFactorNames))
	for _, name := range stubFactorNames {
		vector = append(vector, Factor{Name: name, Value: StubValue, Unavailable: StubUnavailable})
	}
	return Assessment{
		Version:      StubVersion,
		Number:       StubValue,
		HumanDecides: true,
		Vector:       vector,
	}, nil
}
