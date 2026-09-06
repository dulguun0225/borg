package gate

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/score"
)

// [Gate.Fire], one item's build against one row: what fires it, what it reads
// before it appends anything, and the open event it writes. set.go is the same
// firing over a set instead of one item's build.

// Firing is what fires the gate: the row, the records it decides over, what the
// acceptance criteria in force produced against the build, the derivations that
// could not derive, and the build's measurement, which the component that built
// took and the score cannot read.
type Firing struct {
	Row           Row
	ItemID        string
	BuildID       string
	ArtifactID    string
	ServiceID     string
	AreaID        string
	EnvironmentID string
	// ReleaseID is the release a deploy row would put on the environment, and is
	// empty at every row above the merge.
	ReleaseID string
	// ReplacesReleaseID is the release this deploy replaces, and is empty on a
	// service's first release — which has no control whatever the score prefers,
	// there being no build being replaced.
	ReplacesReleaseID string
	// CriteriaInForce is how many criteria the build is decided against. It is
	// separate from Criteria because at the candidate deploy row the count is
	// known and no outcome is: the run that decides them is what that deploy is
	// for. Nothing in the score reads it — test coverage is deliberately not a
	// factor — and it is on the open event so a human at the merge row reads
	// what the run was decided against.
	CriteriaInForce int
	// Criteria is what deciding each of them produced, and is empty at the
	// candidate deploy row.
	Criteria []CriterionResult
	// CouldNotDerive is every derivation that produced no result, each one of
	// [Derivations] and each putting a human at the merge row.
	CouldNotDerive []string
	Measurement    score.Measurement
	// Exposure is what the change reaches that the service did not reach before,
	// read from the same diff by the component that built: the calls, the
	// credentials, the authorization checks and the dependency changes.
	Exposure score.ExposureEvidence
	// Fleet is what a version of what an agent is told is scored on where the
	// change group cannot be computed, and is empty at every row but a role
	// prompt or a skill.
	Fleet score.FleetChange
	// RoutedTo is the routing a record carries for the rows whose routing is not
	// the design's: a safeguard's withdrawal, and the shortening of
	// decision-log retention.
	RoutedTo RoutedTo

	// supersedes is the open event an Edit in place supersedes, set by
	// [Gate.EditInPlace] and by nothing else: a caller cannot supersede a row by
	// composing a firing.
	supersedes string
	// referredFrom and referrers are set by [Gate.Refer] for the same reason.
	referredFrom string
	referrers    []string
}

// Subjects is what the firing's holds are computed against.
func (f Firing) Subjects() Subjects {
	return Subjects{
		Row: f.Row, ItemID: f.ItemID, BuildID: f.BuildID, ServiceID: f.ServiceID,
		AreaID: f.AreaID, EnvironmentID: f.EnvironmentID, ReleaseID: f.ReleaseID,
	}
}

// Fire fires the gate: it reads the intent's state and the rows already pending,
// asks the score about the change and the policy about the values in force,
// recomputes the holds, decides whether a human decides and what mark says so,
// picks the rollout strategy where the row picks one, composes the opening
// payload, and appends the open event as the gate component.
//
// The vector is written here and never recomputed, because it has to exist while
// a human is deciding and the score version moves as outcomes arrive.
func (g *Gate) Fire(ctx context.Context, f Firing) (Opened, error) {
	if err := complete(f); err != nil {
		return Opened{}, err
	}
	if err := g.intentPermits(ctx, f); err != nil {
		return Opened{}, err
	}
	if err := g.nothingPending(ctx, f); err != nil {
		return Opened{}, err
	}

	version, digest, author, err := g.artifactUnderDecision(ctx, f)
	if err != nil {
		return Opened{}, err
	}

	subjects := policy.Subjects{
		GateRow:       f.Row.String(),
		EnvironmentID: f.EnvironmentID,
		ServiceID:     f.ServiceID,
		AreaID:        f.AreaID,
	}
	assessment, err := g.assess(ctx, f, subjects, version)
	if err != nil {
		return Opened{}, err
	}

	applied, err := g.policy.AtGate(ctx, component(f.Row), subjects)
	if err != nil {
		return Opened{}, fmt.Errorf("gate: reading what applies at %s: %w", f.Row, err)
	}

	mismatch, err := g.mismatch(ctx, f)
	if err != nil {
		return Opened{}, err
	}
	holds, err := g.standingHolds(ctx, f.Subjects())
	if err != nil {
		return Opened{}, err
	}
	unmeasured, err := g.unmeasured(ctx, f)
	if err != nil {
		return Opened{}, err
	}

	// The row's own routing is read before the samples, because the review
	// sample's rate is per duty and the duty is what the holds and the row
	// decide together.
	waits, err := g.waitsOn(ctx, f.Row, holds, f.RoutedTo)
	if err != nil {
		return Opened{}, err
	}
	waits.Holders = withoutReferrers(waits.Holders, f.referrers)

	// A row that reads no threshold at all reads no factor set either, so the
	// number decides nothing there and a human is at it always.
	readsNoThreshold := !f.Row.ReadsAThreshold()
	resolved := len(assessment.Resolved) > 0
	overThreshold := f.Row.ReadsAThreshold() && assessment.Number >= applied.Threshold

	selection, err := g.heldOut(ctx, f, subjects, applied, overThreshold, assessment.Resolved)
	if err != nil {
		return Opened{}, err
	}
	reviewSampled, reviewRate, err := g.reviewSampled(ctx, f, subjects, waits,
		overThreshold, resolved, applied.HumanBySafeguard, selection.HeldOut)
	if err != nil {
		return Opened{}, err
	}

	// A held-out item removes the human the number put at the row and no other.
	gatedByNumber := overThreshold && !selection.HeldOut
	marks := marksOn(gatedByNumber, resolved, applied.HumanBySafeguard,
		f.supersedes != "", reviewSampled, readsNoThreshold)

	opened := Opened{
		Gate:       f.Row,
		Subject:    f.Subjects(),
		Assessment: assessment,
		Applied:    applied,
		// A mismatch, a derivation that could not derive, and a service missing
		// one of the deployer's four each put a human at the row without being
		// a mark: the first two are read for what they are, and the third is
		// what says the measurement those fields exist for cannot be read.
		HumanDecides: len(marks) > 0 || mismatch != "" ||
			len(f.CouldNotDerive) > 0 || unmeasured != "",
		Marks:      marks,
		HeldOut:    selection.HeldOut,
		WhyHeldOut: selection.Why,
		Holds:      holds,
		WaitsOn:    waits,
		Mismatch:   mismatch,
		ArtifactID: version,
		Referrers:  f.referrers,
	}
	if !opened.HumanDecides {
		opened.WaitsOn = Waits{}
	}
	opened.Strategy, err = g.strategy(ctx, f, assessment, selection.HeldOut)
	if err != nil {
		return Opened{}, err
	}

	payload, err := json.Marshal(OpeningPayload{
		OpenEvent: score.OpenEvent{
			ItemID:      f.ItemID,
			ArtifactID:  version,
			Gate:        f.Row.String(),
			FactorSet:   assessment.FactorSet,
			Vector:      assessment.Vector,
			Number:      assessment.Number,
			Threshold:   applied.Threshold,
			Resolved:    len(assessment.Resolved),
			HeldOut:     opened.HeldOut,
			HeldOutRate: selection.RateInForce,
			AuthorKey:   author.Key,
			AuthorBasis: string(author.Basis),
		},
		ArtifactDigest:   digest,
		BuildID:          f.BuildID,
		ServiceID:        f.ServiceID,
		AreaID:           f.AreaID,
		EnvironmentID:    f.EnvironmentID,
		ReleaseID:        f.ReleaseID,
		Criteria:         f.Criteria,
		CriteriaInForce:  f.CriteriaInForce,
		CriteriaFailed:   blocked(f.Criteria),
		CouldNotDerive:   f.CouldNotDerive,
		FormulaVersion:   assessment.FormulaVersion,
		Likelihood:       assessment.Likelihood,
		Impact:           assessment.Impact,
		DiscountedImpact: assessment.DiscountedImpact,
		ThresholdFrom:    string(applied.ThresholdFrom),
		Safeguards:       applied.Safeguards,
		Resolutions:      assessment.Resolved,
		HumanDecides:     opened.HumanDecides,
		Marks:            opened.Marks,
		WhyHeldOut:       opened.WhyHeldOut,
		ReviewSampleRate: reviewRate,
		Holds:            opened.Holds,
		Strategy:         opened.Strategy,
		WaitsOn:          opened.WaitsOn,
		Mismatch:         opened.Mismatch,
		Unmeasured:       unmeasured,
		Supersedes:       f.supersedes,
		ReferredFrom:     f.referredFrom,
		Referrers:        f.referrers,
	})
	if err != nil {
		return Opened{}, fmt.Errorf("gate: marshalling the opening payload: %w", err)
	}

	row, err := g.log.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor:         component(f.Row),
		Payload:       string(payload),
		FormatVersion: decisionFormatVersion,
		PolicyVersion: applied.PolicyVersion,
		ScoreVersion:  assessment.Version,
	})
	if err != nil {
		return Opened{}, err
	}
	opened.Row = row
	return opened, nil
}

// EditInPlace is a human authoring a new version while the row is still waiting.
// It appends a new open event naming the version and the row it supersedes, with
// a vector computed from what is now under decision, and an abandonment ends the
// superseded row — in that order, so that a failure between them leaves a row
// pending rather than a decision with nothing deciding it.
//
// The new row waits on another holder of the row's duty, or on the editor where
// none exists, however the recomputed number moves: [MarkEditInPlace] is what
// says so.
func (g *Gate) EditInPlace(ctx context.Context, superseded Opened, f Firing) (Opened, error) {
	if superseded.Gate.Kind == KindDecomposition {
		return Opened{}, fmt.Errorf("%w: %s decides a set, which [Gate.EditSetInPlace] supersedes",
			ErrEditInPlaceRefused, superseded.Gate)
	}
	if !superseded.Gate.ArtifactGate() {
		return Opened{}, fmt.Errorf("%w: %s decides no document", ErrEditInPlaceRefused, superseded.Gate)
	}
	f.supersedes = superseded.Row.ID
	opened, err := g.Fire(ctx, f)
	if err != nil {
		return Opened{}, err
	}
	if _, err := g.abandon(ctx, superseded, component(superseded.Gate), AbandonedBySupersession); err != nil {
		return opened, err
	}
	return opened, nil
}

// assess is what the score answers about the change. A row that reads no
// threshold reads no factor set either, so nothing is assessed there: a human is
// at it always, and what the opening names is the score version in force and no
// vector.
//
// The exposure bound is read here and handed to the score with the change: the
// score supplies a value for that row and may not read what an owner authored,
// so the gate is where the two are put together.
func (g *Gate) assess(ctx context.Context, f Firing, subjects policy.Subjects, version string) (score.Assessment, error) {
	if !f.Row.ReadsAThreshold() {
		return score.Assessment{Version: g.score.Version().ID}, nil
	}
	set, err := FactorSetAt(f.Row)
	if err != nil {
		return score.Assessment{}, err
	}
	bound, err := g.policy.ExposureBound(ctx, subjects)
	if err != nil {
		return score.Assessment{}, fmt.Errorf("gate: reading the exposure bound in force: %w", err)
	}
	assessment, err := g.score.Assess(ctx, score.Change{
		ItemID:           f.ItemID,
		ServiceID:        f.ServiceID,
		AreaID:           f.AreaID,
		ArtifactID:       version,
		FactorSet:        set,
		AtImplementation: f.Row.Kind == KindImplementation,
		AtSpec:           f.Row.Kind == KindSpec,
		ExposureBound:    bound.Number,
		Measurement:      f.Measurement,
		Exposure:         f.Exposure,
		Fleet:            f.Fleet,
	})
	if err != nil {
		return score.Assessment{}, fmt.Errorf("gate: assessing the change: %w", err)
	}
	return assessment, nil
}

// blocked is how many of the criteria decided against the build stop it at the
// Merge to master gate. Undecided is counted with failed, which is what the
// design says of it, and an unreliable criterion reads as absent.
func blocked(criteria []CriterionResult) int {
	n := 0
	for _, c := range criteria {
		if c.Outcome.Blocks(c.Unreliable) {
			n++
		}
	}
	return n
}
