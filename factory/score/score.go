package score

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
)

// component is the actor every row this package appends is attributed to: the
// score writing its own version, and not any human or any other component.
var component = record.Actor{Kind: record.KindComponent, Key: "score", Basis: record.BasisClaimed}

// componentPrincipal is who every read this package makes of the decision log
// is made as: the score calls as itself, and the read event names it.
var componentPrincipal = principal.OfComponent("score")

// ErrChangeIncomplete is returned by [Score.Assess] for a change naming no item
// or no service. Every firing has both, so a blank is a caller's defect and not
// a factor to resolve.
var ErrChangeIncomplete = errors.New("score: a change names an item and a service")

// ChurnWindow is how far back area churn is read. Two weeks is long enough that
// a service shipping weekly registers and short enough that an area quiet since
// last quarter reads as quiet.
const ChurnWindow = 14 * 24 * time.Hour

// Measurement is what the component that built the change already knows and the
// score cannot read: the build's diff, taken where the repository is, or the set
// decomposition proposed where there is no build yet. It is not a record: the
// vector computed from it is, and a diff re-taken later against a repository
// other items have merged into is not the diff the decision was made on.
//
// Unavailable is why the diff could not be taken, and empty where it was. A
// measurement that could not be taken resolves the size and reach factors, which
// puts a human at the gate. A measurement of a proposed set is never unavailable:
// at Decomposition the change group is computed from the set decomposition
// proposed, and a set that exists is a set that was read.
type Measurement struct {
	LinesChanged int
	FilesChanged int
	FilesInTree  int
	// RequirementsProposed and ServicesProposed are the set decomposition
	// proposed: how many of the intent's requirements the set answers and how
	// many services it spans. They are what the change group is computed from at
	// Decomposition, where there is no build and no diff.
	RequirementsProposed int
	ServicesProposed     int
	// DestroysStoredData is whether the diff destroys stored data, which
	// resolves the reversibility factor at Implementation rather than valuing it.
	DestroysStoredData bool
	// DestroysStoredDataUnavailable is why nothing could say whether the diff
	// destroys stored data, and is empty where something did. The reading is per
	// toolchain the way the exposure list is, so a toolchain with no derivation
	// behind it resolves the reversibility factor rather than reading the diff as
	// destroying nothing.
	DestroysStoredDataUnavailable string
	Unavailable                   string
}

// FromProposedSet reports whether this measurement is the set decomposition
// proposed rather than a build's diff.
func (m Measurement) FromProposedSet() bool {
	return m.RequirementsProposed > 0 || m.ServicesProposed > 0
}

// ExposureEvidence is the exposure list: what the change reaches that the
// service did not reach before, derived from the diff and the build's resolved
// set diffed against the current release's build. Each entry names the call, the
// credential, the check or the package with its version and licence, and the
// file and line, because what a human at Implementation argues with is that list
// beside the diff.
//
// It is handed in for the reason the diff is: the derivation is per toolchain
// and runs where the repository and the two builds are. Unavailable is set where
// no extractor runs for the toolchain, which resolves the factor rather than
// reading it as nothing — a diff adding none of this reads as nothing, and an
// extractor that did not run does not.
//
// Derived is what separates the two, and it is a field rather than a length
// check because the zero value of a slice is what a caller that read nothing
// hands over: an evidence nobody derived is unavailable, never an empty list. A
// caller whose extractor ran and found none of this sets Derived and leaves the
// four lists empty, which is the reading that lowers the number.
type ExposureEvidence struct {
	Derived             bool
	OutboundCalls       []string
	Credentials         []string
	AuthorizationChecks []string
	DependencyChanges   []string
	Unavailable         string
}

// List is every entry of the evidence in one list, which is what the vector
// carries.
func (e ExposureEvidence) List() []string {
	return slices.Concat(e.OutboundCalls, e.Credentials, e.AuthorizationChecks, e.DependencyChanges)
}

// FleetChange is what a version of what an agent is told is scored on where the
// change group cannot be computed: the share of the factory working from the
// version in force this one replaces, and how far this version departs from it.
// Both are the caller's, the fleet's records being the row's own.
// Derived is whether the fleet's records were read for this firing, and carries
// what it carries on [ExposureEvidence]: a share of nothing and a departure of
// nothing are what a caller that read no record hands over, and reading them as
// a version nobody works from and a version that departs in nothing would be the
// lowest reading there is on the row the factory knows least about.
type FleetChange struct {
	Derived            bool
	ShareWorkingFromIt float64
	Departure          float64
	Unavailable        string
}

// Change is what the score is asked about. The ids name records this package
// reads; the measurement, the exposure evidence and the fleet reading are what
// the caller already knows and the score cannot read.
//
// The build is not among them, and that is deliberate: nothing here reads a
// build record — what the score would want from one is the diff and the resolved
// set, which are the measurement and the exposure evidence, taken where the
// repository is. What says which build a vector was computed over is the open
// event the gate writes it onto.
type Change struct {
	ItemID    string
	ServiceID string
	AreaID    string
	// ArtifactID is the version under decision, whose author the prior is kept
	// on. Where it is empty the prior reads the item's newest implementation
	// version instead.
	ArtifactID string
	// FactorSet is which of the three sets this firing is scored on, which the
	// caller decides because a gate row is package gate's vocabulary. A change
	// naming none is refused.
	FactorSet FactorSet
	// AtImplementation is whether this firing is the Implementation row. Three
	// resolutions bind there and at no row above it: an irreversible hazard
	// severity, a diff that destroys stored data, and an exposure over the bound.
	AtImplementation bool
	// AtSpec is whether this firing is the Spec row, where an intent grouped
	// from reports resolves the source value and a withdrawal that removes a
	// protection resolves the withdrawal value.
	AtSpec bool
	// AtDeployToProduction is whether this firing is the deploy to production
	// row. An irreversible hazard severity resolves there where no control can
	// run: there is then no schedule to pick and every deploy goes without one,
	// so the decision is a human's whatever the formula returns.
	AtDeployToProduction bool
	// ReplacesReleaseID is the release this deploy replaces, and is empty on a
	// service's first release, which has no build to keep serving beside it and
	// so no control whatever the score prefers.
	ReplacesReleaseID string
	// EveryTargetServesAShare is whether every target of the environment this
	// deploy reaches is behind a platform that decides what fraction of arriving
	// traffic reaches each of two builds. Where one does not, the row with a
	// control is unavailable and every deploy there goes without one.
	EveryTargetServesAShare bool
	// ExposureBound is the bound in force on this service, which the caller read
	// through package policy: the score supplies a value for that row and may
	// not read what an owner authored.
	ExposureBound float64
	Measurement   Measurement
	Exposure      ExposureEvidence
	Fleet         FleetChange
}

// OpenEvent is the part of a decision's open event this package reads back: the
// item the decision was about, the artifact version under decision, the row it
// fired at, the factor set it was scored on, the number and the threshold it was
// decided against, whether the vector resolved anything, and whether the score's
// own sample had selected the item. Package gate composes it into the payload it
// writes, so every one of those field names is declared once and an outcome the
// score counts is an outcome a gate wrote.
type OpenEvent struct {
	ItemID     string    `json:"item_id"`
	ArtifactID string    `json:"artifact_id"`
	Gate       string    `json:"gate"`
	FactorSet  FactorSet `json:"factor_set"`
	// Vector is the assessment's vector: every factor this firing weighed or
	// resolved, which is what the calibration and the bands read back — the
	// number alone cannot say which factor moved or which was resolved.
	Vector    []Factor `json:"vector,omitempty"`
	Number    float64  `json:"number"`
	Threshold float64  `json:"threshold"`
	// Resolved is how many factors this firing resolved. A firing with any is a
	// human's whatever the number read, so the calibration counts it apart.
	Resolved int `json:"resolved"`
	// HeldOut is whether the score selected this item into its sample. It is
	// written on every decision on the item from the selection onward, which is
	// what makes an item selected once auto-pass every gate the score would have
	// gated.
	HeldOut bool `json:"held_out"`
	// HeldOutRate is the rate in force at the selection, after every safeguard
	// clamping it on that item's service, project and area, so a held-out outcome
	// can be weighted by the probability that selected it.
	HeldOutRate float64 `json:"held_out_rate,omitempty"`
	// AuthorKey and AuthorBasis are who authored the version under decision and
	// whether anything verified that key. A claimed row is learned from as a row
	// that says so.
	AuthorKey   string `json:"author_key,omitempty"`
	AuthorBasis string `json:"author_basis,omitempty"`
	// Authored is what an agent that authored the version under decision worked
	// from: the input manifest, the effort its entry ran at, and the versions of
	// the role prompt and the skills. It is empty where a human authored, and
	// where nothing read the run. They are the facts the per-author prior
	// deliberately averages over, put where a human arguing with the number can
	// see them and a query can group decisions by them without any of it moving
	// the prior.
	Authored Authored `json:"authored,omitzero"`
}

// CloseEvent is the part of a decision's close event this package reads back: the
// verdict, what auto-passed the firing where the factory decided for itself, and
// what a human's rejection named. The three are read together because none
// answers a question on its own — an approval by a human is evidence about an
// author, an approval by the factory is the factory agreeing with itself unless
// its own sample is what passed it, and a rejection moves nothing until it has
// resolved.
type CloseEvent struct {
	Verdict         string `json:"verdict"`
	WhyItAutoPassed string `json:"why_it_auto_passed"`
	// RejectionNamed is what the human named in the rejection, which is what a
	// re-authored version is compared against by content digest.
	RejectionNamed string `json:"rejection_named,omitempty"`
}

// The two verdicts this package reads off a close event. Package gate owns the
// vocabulary and cannot be imported here, importing this package itself, so these
// are the two words this package reads and TestTheVerdictsGateWritesAreTheOnesTheScoreReads
// in that package is what holds the two spellings together.
const (
	// VerdictApproved is a decision approved.
	VerdictApproved = "approve"
	// VerdictRejected is a decision rejected. A hold is neither, and teaches the
	// score nothing.
	VerdictRejected = "reject"
)

// Score is the risk score over one pool, computing every vector under one
// version — the one in force when the process started, which every decision it
// produces names.
type Score struct {
	pool        *pgxpool.Pool
	version     Version
	draw        Draw
	marks       Marks
	authorship  Authorship
	withdrawals Withdrawals
	token       lease.Token
}

// Composition is what [New] is handed. It is a struct and not a list of
// arguments because five of the seven are interfaces of one shape — something
// the score reads and does not own — and a caller passing one where another
// belongs would compile.
type Composition struct {
	Pool    *pgxpool.Pool
	Version Version
	// Draw is where the held-out sample's randomness comes from. A nil draw is
	// [RandomDraw]: a factory composed without one still holds a sample out,
	// because a factory that quietly stopped sampling would be one whose
	// threshold could only ever fall.
	Draw Draw
	// Marks is the rollbacks a named human at Ops marked. A nil marks is
	// [NoMarks].
	Marks Marks
	// Authorship is what an agent authoring a version worked from. A nil one is
	// [NoAuthorship].
	Authorship Authorship
	// Withdrawals is what a spec version under decision removes. A nil one is
	// [NoWithdrawals].
	Withdrawals Withdrawals
	Token       lease.Token
}

// New returns the score over the composition, computing every vector under the
// version it names and fencing every read it makes of the decision log with its
// token.
func New(c Composition) *Score {
	if c.Draw == nil {
		c.Draw = RandomDraw{}
	}
	if c.Marks == nil {
		c.Marks = NoMarks{}
	}
	if c.Authorship == nil {
		c.Authorship = NoAuthorship{}
	}
	if c.Withdrawals == nil {
		c.Withdrawals = NoWithdrawals{}
	}
	return &Score{
		pool: c.Pool, version: c.Version, draw: c.Draw, marks: c.Marks,
		authorship: c.Authorship, withdrawals: c.Withdrawals, token: c.Token,
	}
}

// Version is the score version every assessment this score produces names.
func (s *Score) Version() Version { return s.version }

// Assess is the score's answer for one change under the version this score was
// composed with, which is the newest one. A gate an authored threshold binds is
// decided under the version in force at its own scope instead, and asks through
// [Score.AssessUnder].
func (s *Score) Assess(ctx context.Context, c Change) (Assessment, error) {
	return s.AssessUnder(ctx, s.version, c)
}

// AssessUnder is the score's answer for one change under one version: the
// factors of its set read, the published formula that version names applied over
// the ones that were computable under the weights that version gives them, and
// every resolution recorded. It reads records and never writes one.
//
// The version is a parameter because a version that redefined the number does
// not decide a gate an authored threshold binds until its owner has confirmed
// it, so the version a firing computes under is the one in force at that gate's
// scope and not always the newest. What resolves that is [InForceAt], and the
// caller that read it hands the answer here.
func (s *Score) AssessUnder(ctx context.Context, version Version, c Change) (Assessment, error) {
	if c.ItemID == "" || c.ServiceID == "" {
		return Assessment{}, fmt.Errorf("%w: item %q, service %q", ErrChangeIncomplete, c.ItemID, c.ServiceID)
	}
	definitions := definitionsOf(c.FactorSet)
	if definitions == nil {
		return Assessment{}, fmt.Errorf("%w: %q", ErrFactorSetUnknown, c.FactorSet)
	}
	weights := version.WeightsOf(c.FactorSet)

	vector := make([]Factor, 0, len(definitions))
	var resolutions []Resolution
	for _, d := range definitions {
		r, err := d.read(s, ctx, c)
		if err != nil {
			return Assessment{}, fmt.Errorf("score: reading %s: %w", d.name, err)
		}
		f := Factor{
			Name: d.name, Group: d.group, Term: d.term, Weight: weights.Of(d.name),
			Reading: r.words, Level: r.level, Evidence: r.evidence,
			Width: r.width, Closes: r.closes, Claimed: r.claimed, Verified: r.verified,
		}
		switch {
		case r.unavailable != "":
			resolve(&f, &resolutions, CauseUnavailable, r.unavailable)
		case r.resolved != "":
			resolve(&f, &resolutions, r.cause, r.resolved)
		case version.Drifted(d.name):
			resolve(&f, &resolutions, CauseDrifted,
				"calibration found this factor drifted, so it is resolved until a recalibration is in force at this gate")
		}
		vector = append(vector, f)
	}

	authored, err := s.authorship.OfArtifact(ctx, c.ArtifactID)
	if err != nil {
		return Assessment{}, fmt.Errorf("score: reading what authored %s: %w", c.ArtifactID, err)
	}

	a := Assessment{
		Version:        version.ID,
		FormulaVersion: version.FormulaVersion,
		FactorSet:      c.FactorSet,
		Vector:         vector,
		Resolved:       resolutions,
		Authored:       authored,
		ControlBound:   version.ControlBoundOrShipped(),
	}
	a.Likelihood, a.Impact, a.DiscountedImpact, a.Number = reduce(vector)
	return a, nil
}
