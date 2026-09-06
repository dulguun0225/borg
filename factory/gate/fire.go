package gate

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/screenstatemachine"
)

// [Gate.Fire], one item's build against one row: what fires it, what it reads
// before it appends anything, and the open event it writes. set.go is the same
// firing over a set instead of one item's build.

// Firing is what fires the gate: the row, the records it decides over, what the
// acceptance criteria in force produced against the build, the derivations that
// could not derive, and the build's measurement, which the component that built
// took and the score cannot read.
type Firing struct {
	Row Row
	// RecordID is the record this row decides where it decides one rather than
	// an item: the safeguard whose withdrawal is under decision, the halt, the
	// legal hold, or the factory-wide settings record a shortening of
	// decision-log retention moves. It is required at those four rows and
	// refused at every other, and it is what the check that nothing is already
	// pending matches on there.
	RecordID      string
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
	// Screens is what the transition check derived from the build, per screen in
	// force: the transitions the extractor can show the implementation admits,
	// or the cause it could not derive them. It is read at the Implementation
	// row and at no other. A screen it could not derive resolves a factor rather
	// than rejecting, and [ScreenRejection] is what rejects over the rest.
	Screens screenstatemachine.Derivation
	// RoutedTo is the routing a record carries for the rows whose routing is not
	// the design's: a safeguard's withdrawal, and the shortening of
	// decision-log retention.
	RoutedTo RoutedTo
	// RevertWhileRollbackHolds is whether the item under decision is the revert
	// of a rollback that has not shipped yet. Where it holds and a human decides
	// the row, the service runs the build the rollback restored, master still
	// contains the defect, and nothing ships past that human — which is what
	// [Opened.Pages] reads it for.
	//
	// It is the caller's answer and not a hold recomputed here: the hold a
	// rollback leaves stands on every item of the service except this one, and
	// what says which item is the revert is the intent the rollback's own deploy
	// record leads to, which this package does not walk.
	RevertWhileRollbackHolds bool
	// PriorsRestarted is every author whose per-author prior stands drifted and
	// whose held-out decisions the cut a shorter retention value permits would
	// remove, which is the prior the score restarts when those decisions go. It
	// is named at the row that decides a shortening of decision-log retention
	// and refused at every other, and the composition computes it: what it is
	// read from is the log's own rows and the author of the version each names,
	// which this package does not walk.
	PriorsRestarted []string

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
		Row: f.Row, RecordID: f.RecordID,
		ItemID: f.ItemID, BuildID: f.BuildID, ServiceID: f.ServiceID,
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
	// The policy first, because it answers which score version decides this row:
	// a version that redefined the number does not decide a gate an authored
	// threshold binds until its owner has confirmed it, so the vector is computed
	// under the version in force at this scope and the decision names that one.
	applied, err := g.policy.AtGate(ctx, componentPrincipal(f.Row), subjects)
	if err != nil {
		return Opened{}, fmt.Errorf("gate: reading what applies at %s: %w", f.Row, err)
	}

	rollout, err := g.rollout(ctx, f)
	if err != nil {
		return Opened{}, err
	}

	assessment, err := g.assess(ctx, f, subjects, version, applied, rollout)
	if err != nil {
		return Opened{}, err
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
	// decide together. A resolution naming a human is what routes it where the
	// record the firing carries names none: a withdrawn protection goes to the
	// human its provenance names rather than to the owner by default.
	routed := f.RoutedTo
	if routed.Human == "" {
		routed.Human = routedByAResolution(assessment.Resolved)
	}
	waits, err := g.waitsOn(ctx, f.Row, holds, routed)
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

	// A held-out item removes the human the number put at the row and no other,
	// and removes none at a firing a safeguard or a resolved factor put one at —
	// which is what [score.Selection.AutoPasses] says and the selection itself
	// does not, the selection standing on every decision from where it was made.
	gatedByNumber := overThreshold && !selection.AutoPasses
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
		Marks:                    marks,
		HeldOut:                  selection.HeldOut,
		WhyHeldOut:               selection.Why,
		Holds:                    holds,
		PriorsRestarted:          f.PriorsRestarted,
		WaitsOn:                  waits,
		Mismatch:                 mismatch,
		RevertWhileRollbackHolds: f.RevertWhileRollbackHolds,
		ArtifactID:               version,
		Referrers:                f.referrers,
	}
	if !opened.HumanDecides {
		opened.WaitsOn = Waits{}
	}
	rollout.HeldOut = selection.HeldOut
	opened.Strategy = g.strategy(f, assessment, rollout)

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
			Authored:    assessment.Authored,
		},
		ArtifactDigest:           digest,
		RecordID:                 f.RecordID,
		BuildID:                  f.BuildID,
		ServiceID:                f.ServiceID,
		AreaID:                   f.AreaID,
		EnvironmentID:            f.EnvironmentID,
		ReleaseID:                f.ReleaseID,
		Criteria:                 f.Criteria,
		CriteriaInForce:          f.CriteriaInForce,
		CriteriaFailed:           blocked(f.Criteria),
		CouldNotDerive:           f.CouldNotDerive,
		FormulaVersion:           assessment.FormulaVersion,
		Likelihood:               assessment.Likelihood,
		Impact:                   assessment.Impact,
		DiscountedImpact:         assessment.DiscountedImpact,
		ThresholdFrom:            string(applied.ThresholdFrom),
		Safeguards:               applied.Safeguards,
		Resolutions:              assessment.Resolved,
		HumanDecides:             opened.HumanDecides,
		Marks:                    opened.Marks,
		WhyHeldOut:               opened.WhyHeldOut,
		ReviewSampleRate:         reviewRate,
		Holds:                    opened.Holds,
		PriorsRestarted:          f.PriorsRestarted,
		Strategy:                 opened.Strategy,
		WaitsOn:                  opened.WaitsOn,
		Mismatch:                 opened.Mismatch,
		RevertWhileRollbackHolds: opened.RevertWhileRollbackHolds,
		Unmeasured:               unmeasured,
		Supersedes:               f.supersedes,
		ReferredFrom:             f.referredFrom,
		Referrers:                f.referrers,
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
// so the gate is where the two are put together. So is the version the vector is
// computed under, which the policy resolved for this row's own scope.
func (g *Gate) assess(ctx context.Context, f Firing, subjects policy.Subjects, version string,
	applied policy.Applied, rollout score.Rollout) (score.Assessment, error) {

	inForce, err := g.scoreVersion(ctx, applied)
	if err != nil {
		return score.Assessment{}, err
	}
	if !f.Row.ReadsAThreshold() {
		return score.Assessment{Version: inForce.ID}, nil
	}
	set, err := FactorSetAt(f.Row)
	if err != nil {
		return score.Assessment{}, err
	}
	bound, err := g.policy.ExposureBound(ctx, subjects)
	if err != nil {
		return score.Assessment{}, fmt.Errorf("gate: reading the exposure bound in force: %w", err)
	}
	assessment, err := g.score.AssessUnder(ctx, inForce, score.Change{
		ItemID:                  f.ItemID,
		ServiceID:               f.ServiceID,
		AreaID:                  f.AreaID,
		ArtifactID:              version,
		FactorSet:               set,
		AtImplementation:        f.Row.Kind == KindImplementation,
		AtSpec:                  f.Row.Kind == KindSpec,
		AtDeployToProduction:    f.Row.Kind == KindDeployToProduction,
		ReplacesReleaseID:       rollout.ReplacesReleaseID,
		EveryTargetServesAShare: rollout.EveryTargetServesAShare,
		ExposureBound:           bound.Number,
		ScreensNotDerived:       screensNotDerived(f),
		Measurement:             f.Measurement,
		Exposure:                f.Exposure,
		Fleet:                   f.Fleet,
	})
	if err != nil {
		return score.Assessment{}, fmt.Errorf("gate: assessing the change: %w", err)
	}
	return assessment, nil
}

// scoreVersion is the version this firing computes its vector under: the one the
// policy resolved as in force at this row's scope, which is the newest where
// nobody authored a threshold here and the last one confirmed at the scope where
// somebody did. It is read back off the log where the two differ, so the vector,
// the number and the version the decision names are one version's.
func (g *Gate) scoreVersion(ctx context.Context, applied policy.Applied) (score.Version, error) {
	newest := g.score.Version()
	if applied.ScoreVersion == "" || applied.ScoreVersion == newest.ID {
		return newest, nil
	}
	inForce, err := score.Get(ctx, g.pool, g.token, applied.ScoreVersion)
	if err != nil {
		return score.Version{}, fmt.Errorf("gate: reading the score version in force at this row: %w", err)
	}
	return inForce, nil
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
