package healthmonitor

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/window"
)

// failed is the failed exit: a crossing found inside the window, with no human
// involved. The window's close is the last durable step of it and never the
// first — closed first, the same stop would leave a release the factory had
// failed serving production with no rollback deploy record, no hold, no
// mismatch, and nothing that would ever retry. So the order here is the
// rollback's own deploy record and the releases it undid, the incident and the
// revert intent, a page where nothing was rolled back, and this window closed
// failed last.
func (h *HealthMonitor) failed(ctx context.Context, w Watching, one Watched) (Watched, error) {
	one.WhyNoRollback = h.whyNoRollback(ctx, w, one)
	if one.WhyNoRollback == "" {
		if err := h.rollBack(ctx, w, &one); err != nil {
			return one, err
		}
	}

	crossed, err := h.raiseCrossing(ctx, w, one)
	if err != nil {
		return one, err
	}
	one.IncidentID, one.RaisedIntentID = crossed.Incident.ID, crossed.IntentID

	if one.WhyNoRollback != "" {
		if err := h.pageNoRollback(ctx, one); err != nil {
			return one, err
		}
	}

	return h.close(ctx, w, one.Window, window.ExitFailed, one)
}

// whyNoRollback is why the failed exit rolls nothing back, and empty where it
// is about to roll one back: a mismatch standing on the service — the target
// would be computed from records the mismatch already shows wrong — a factory
// composed with no deployer, or no release below this one to return to.
func (h *HealthMonitor) whyNoRollback(ctx context.Context, w Watching, one Watched) string {
	if h.mismatches != nil {
		mismatched, reason, err := h.mismatches.Mismatch(ctx, w.ID)
		if err == nil && mismatched {
			return "a mismatch stands on this service, so no rollback is performed: " + reason
		}
	}
	if h.deployer == nil {
		return "this factory is composed with no deployer to perform one"
	}
	if !one.HasBaseline {
		return "there is no release below this one to return to"
	}
	return ""
}

// rollBack asks the deployer for the rollback: the target is [Watched.Baseline]
// — the rollback's target computed for the release under watch, and not always
// the ordinal predecessor — and every release above the failed one that this
// return also undoes closes its own open window skipped, master being linear
// and this being no choice.
func (h *HealthMonitor) rollBack(ctx context.Context, w Watching, one *Watched) error {
	skippedIDs, err := h.releasesSkippedBy(ctx, w, one.Baseline, one.Release)
	if err != nil {
		return err
	}
	rollback := Rollback{
		ServiceID: w.ID, ServiceName: w.Name, EnvironmentID: w.EnvironmentID,
		ToReleaseID: one.Baseline.ID, ToBuildID: one.Baseline.BuildID,
		FailedReleaseID: one.Release.ID, SkippedReleaseIDs: skippedIDs,
		Source: deploy.SourceHealthMonitorAtFailed,
	}
	if err := h.deployer.RollBack(ctx, rollback); err != nil {
		return fmt.Errorf("healthmonitor: rolling %s back to %s: %w", w.Name, one.Baseline.ID, err)
	}
	one.Rolled, one.Target = &rollback, one.Baseline

	for _, id := range skippedIDs {
		win, found, err := window.ForRelease(ctx, h.pool, id)
		if err != nil {
			return err
		}
		if found && win.Open() {
			if _, err := h.windows.Close(ctx, win.ID, window.ExitSkipped, window.Closing{}); err != nil {
				return err
			}
			one.SkippedWindows = append(one.SkippedWindows, win.ID)
		}
	}
	return nil
}

// releasesSkippedBy is every release above the target other than the failed one,
// which returning to the target undoes as well. Master is linear, so this is not
// a choice: putting the target's build back takes out everything merged after
// it, whether it was merged before the failed release or after it.
//
// It runs to the service's highest release and not to the failed one. A release
// above the failed one is exactly the case the design names — a rollback that
// sweeps — and one below it and above the target is undone for the same reason.
func (h *HealthMonitor) releasesSkippedBy(ctx context.Context, w Watching, target, failed release.Release) ([]string, error) {
	highest, found, err := release.Highest(ctx, h.pool, w.ID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	above, err := release.Between(ctx, h.pool, w.ID, target.Number+1, highest.Number)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, r := range above {
		if r.ID != failed.ID {
			ids = append(ids, r.ID)
		}
	}
	return ids, nil
}

// raiseCrossing is the incident this crossing raises or observes, and the
// intent it raises where it raises one — the revert, keyed on this service and
// this release the way every crossing after the close is.
func (h *HealthMonitor) raiseCrossing(ctx context.Context, w Watching, one Watched) (Crossed, error) {
	failureRecords, err := h.failureRecordsJSON(ctx, w, one.Window, one.Evaluated.Crossed)
	if err != nil {
		return Crossed{}, err
	}
	statement := fmt.Sprintf("%s's release %d failed its analysis window and was rolled back to release %d",
		w.Name, one.Release.Number, one.Baseline.Number)
	if one.WhyNoRollback != "" {
		statement = fmt.Sprintf("%s's release %d failed its analysis window; no rollback was performed (%s)",
			w.Name, one.Release.Number, one.WhyNoRollback)
	}
	return h.recordCrossing(ctx, w, one.Release, one.Window.DeployID, one.Evaluated.Crossed,
		one.Window.PolicyVersion, one.Window.ScoreVersion, failureRecords, statement)
}

// failureRecordsJSON is the failure records for the crossing's own service,
// release and target, encoded the way the incident stores them: a field of it
// rather than a link to the store.
func (h *HealthMonitor) failureRecordsJSON(ctx context.Context, w Watching, win window.Window, cross *Crossing) (string, error) {
	if cross == nil {
		return "", nil
	}
	records, err := h.emission.FailureRecords(ctx, Reading{
		ServiceName: w.Name, Target: cross.Target,
		Release: Arm{BuildID: win.BuildID, DeployID: win.DeployID},
	})
	if err != nil {
		return "", fmt.Errorf("healthmonitor: reading the failure records for %s: %w", w.Name, err)
	}
	if len(records) == 0 {
		return "", nil
	}
	encoded, err := json.Marshal(records)
	if err != nil {
		return "", fmt.Errorf("healthmonitor: encoding the failure records for %s: %w", w.Name, err)
	}
	return string(encoded), nil
}

// pageNoRollback is the page a failed exit that rolled nothing back fires:
// production is running a release the factory has just failed, and no
// mechanism the factory has will improve it. It fires whatever [HealthMonitor]
// raised from the crossing, because raising the item does not answer the page.
func (h *HealthMonitor) pageNoRollback(ctx context.Context, one Watched) error {
	if h.pager == nil {
		return nil
	}
	_, err := h.pager.Notify(ctx, notifier.Wait{
		Row:  one.Window.ID,
		Kind: notifier.KindFailedWithNoRollback,
		Waiting: fmt.Sprintf("release %d failed its analysis window and no rollback was performed: %s",
			one.Release.Number, one.WhyNoRollback),
		ServiceID: one.Window.ServiceID,
		// The kind meets the page's condition by definition, and a wait that
		// denied it would be refused where it is delivered.
		Worse: true,
		// Production serves a release this component has just failed and no
		// rollback has run, which is the whole of the design's first kind: the
		// software is worse for every hour of the wait, so the page fires at
		// whatever hour the condition arose and never waits for the service's
		// paging hours.
		RollbackOutstanding: true,
	})
	return err
}

// Crossed is what [HealthMonitor.recordCrossing] produced: the incident —
// freshly raised or observed again — and the intent it raised, empty where the
// crossing observed an incident already open.
type Crossed struct {
	Incident incident.Incident
	IntentID string
}

// recordCrossing is the health monitor's own deduplication, ahead of intake: an
// open incident on this service and this release makes a further crossing an
// observation on it and never a second intent. A fresh incident raises the
// intent this crossing raises — the revert inside the window, or, after it has
// closed, an unrefined intent taking the same stages and the same gates as one
// an owner typed — keyed by the evidence of this service and this release, so a
// second crossing before decomposition has run still attaches to the one
// already raised rather than opening a second.
func (h *HealthMonitor) recordCrossing(ctx context.Context, w Watching, rel release.Release, deployID string,
	cross *Crossing, policyVersion, scoreVersion, failureRecords, statement string) (Crossed, error) {
	if cross == nil {
		return Crossed{}, nil
	}
	existing, found, err := incident.Open(ctx, h.pool, w.ID, rel.ID)
	if err != nil {
		return Crossed{}, err
	}
	if found {
		observed, err := h.incidents.Observe(ctx, existing.ID)
		return Crossed{Incident: observed, IntentID: observed.IntentID}, err
	}

	intentID, err := h.raiseOrFindIntent(ctx, w, rel, statement)
	if err != nil {
		return Crossed{}, err
	}

	reading, size, confidence, runLength := crossingBoundary(cross)
	raised, err := h.incidents.Raise(ctx, Actor, incident.Raising{
		EnvironmentID: w.EnvironmentID, ServiceID: w.ID, ReleaseID: rel.ID, DeployID: deployID,
		Reading: reading, Quantity: string(cross.Quantity), Size: size, Confidence: confidence, RunLength: runLength,
		BoundaryVersion: boundary.Version, PolicyVersion: policyVersion, ScoreVersion: scoreVersion,
		FailureRecords: failureRecords, IntentID: intentID,
	})
	return Crossed{Incident: raised, IntentID: intentID}, err
}

// raiseOrFindIntent is the intent an incident raises through intake: the oldest
// one already open on this evidence, or a fresh one where there is none. It is
// what keeps the second crossing on one release before decomposition has run
// from raising a second intent for it, beside the incident's own dedup.
func (h *HealthMonitor) raiseOrFindIntent(ctx context.Context, w Watching, rel release.Release, statement string) (string, error) {
	evidence := intent.Evidence{ServiceID: w.ID, ReleaseID: rel.ID}
	waiting, found, err := intent.OnEvidence(ctx, h.pool, evidence)
	if err != nil {
		return "", err
	}
	if found {
		return waiting.ID, nil
	}
	if h.intake == nil {
		return "", nil
	}
	taken, err := h.intake.TakeIn(ctx, Actor, intent.Arrival{
		Source: intent.SourceDetector, Statement: statement, Evidence: evidence,
	})
	if err != nil {
		return "", err
	}
	return taken.ID, nil
}

// crossingBoundary is a [Crossing] read back as what the incident names: which
// reading crossed, the size, and the confidence or the run length in force
// according to whether that reading closes.
func crossingBoundary(cross *Crossing) (reading incident.Reading, size, confidence, runLength float64) {
	reading = incident.ReadingComparison
	switch cross.Kind {
	case KindOwnHistory:
		reading = incident.ReadingOwnHistory
	case KindExplicitThreshold:
		reading = incident.ReadingExplicitThreshold
	}
	if reading.StatesARunLength() {
		return reading, cross.Boundary.Size, 0, cross.Boundary.RunLength()
	}
	return reading, cross.Boundary.Size, cross.Boundary.Confidence, 0
}
