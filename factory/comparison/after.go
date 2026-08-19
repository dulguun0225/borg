package comparison

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/window"
)

// AfterWindow reads the release running in production whose window has already
// closed, and returns false where there is nothing to read — no completed deploy, or
// a release still under watch, which is [Comparison.Watch]'s.
//
// The comparison keeps running after the window closes. What it finds then is not a
// rollback candidate: the change has been live for a week and the window's authority
// ended long before. It is an incident and an unrefined intent, taking the same
// stages and the same gates as an intent an owner typed — which is the whole of
// "finds issues and fixes bugs".
//
// The second crossing on one release is an observation on the incident already
// open and never a second intent. That is what keeps a service failing steadily from
// filling Work with one item per pass, and what it costs is that the count of
// observations is all a reader has about how long it has been crossing.
func (c *Comparison) AfterWindow(ctx context.Context, w Watching) (Reading, bool, error) {
	if err := w.validate(); err != nil {
		return Reading{}, false, err
	}

	current, running, err := deploy.Current(ctx, c.pool, w.ID, w.EnvironmentID)
	if err != nil || !running {
		return Reading{}, false, err
	}
	win, watched, err := window.ForRelease(ctx, c.pool, current.ReleaseID)
	if err != nil {
		return Reading{}, false, err
	}
	if watched && win.Open() {
		// The window is still open, so this release is the window's own business and
		// the window's authority still runs. Reading it here as well would evaluate one
		// release twice in one pass and could condemn it and raise an item for it at
		// once.
		return Reading{}, false, nil
	}

	rel, err := release.Get(ctx, c.pool, current.ReleaseID)
	if err != nil {
		return Reading{}, false, err
	}
	reading := Reading{Release: rel}

	observed, baseline, hasBaseline, err := c.observe(ctx, w, rel)
	if err != nil {
		return reading, true, err
	}
	reading.Observed, reading.Baseline, reading.HasBaseline = observed, baseline, hasBaseline

	// The boundary the closed window was read against, so what condemns a release
	// after its window is the same arithmetic that would have condemned it inside it.
	// A release that was never watched — which nothing on this path produces, a
	// production deploy always opening one — is read against what is in force now.
	b := boundary.Boundary{Size: win.Size, Confidence: win.Confidence}
	if !watched {
		parameters, err := c.policy.WindowParameters(ctx, w.ID)
		if err != nil {
			return reading, true, err
		}
		b = boundary.Boundary{Size: parameters.Size.Number, Confidence: parameters.Confidence.Number}
	}
	reading.Boundary, err = b.Evaluate(observed)
	if err != nil {
		return reading, true, err
	}
	if !reading.Boundary.Harm {
		return reading, true, nil
	}

	raised, err := c.recordCrossing(ctx, w, rel, current.ID, false)
	if err != nil {
		return reading, true, err
	}
	reading.IncidentID, reading.RaisedIntentID = raised.IncidentID, raised.IntentID
	reading.WhyNoRollback = "the window over this release has closed, so the factory's own authority to roll it back has ended"
	return reading, true, nil
}

// ResolveSettled resolves every open incident of the service whose two conditions
// hold: the crossing has stopped against what is running, and what was raised from
// it has finished. Both are facts about other records, which is why the incident's
// own writer checks neither.
//
// A rollback does not reach either condition on its own. It stops the crossing
// against the condemned release and leaves production worse than it should be, which
// is what the hold and the page both say — and what was raised from it is the revert,
// which has to ship.
//
// It returns the incidents it resolved. An incident it cannot resolve is not an
// error: this is a pass over what is settled, and everything else is still open.
func (c *Comparison) ResolveSettled(ctx context.Context, w Watching) ([]incident.Incident, error) {
	if err := w.validate(); err != nil {
		return nil, err
	}
	all, err := incident.ForService(ctx, c.pool, w.ID)
	if err != nil {
		return nil, err
	}

	var resolved []incident.Incident
	for _, i := range all {
		if !i.Open() {
			continue
		}
		stopped, err := c.crossingStopped(ctx, w, i)
		if err != nil {
			return resolved, err
		}
		if !stopped {
			continue
		}
		shipped, err := c.raisedHasShipped(ctx, w, i)
		if err != nil {
			return resolved, err
		}
		if !shipped {
			continue
		}
		settled, err := c.incidents.Resolve(ctx, i.ID)
		if err != nil {
			return resolved, err
		}
		resolved = append(resolved, settled)
	}
	return resolved, nil
}

// crossingStopped is whether the release the incident is about is still running and
// still crossing. A release that is no longer running is not crossing against what is
// running, which is the condition as the design words it: the release was rolled
// back, or a later release replaced it.
func (c *Comparison) crossingStopped(ctx context.Context, w Watching, i incident.Incident) (bool, error) {
	current, running, err := deploy.Current(ctx, c.pool, w.ID, w.EnvironmentID)
	if err != nil {
		return false, err
	}
	if !running || current.ReleaseID != i.ReleaseID {
		return true, nil
	}

	rel, err := release.Get(ctx, c.pool, i.ReleaseID)
	if err != nil {
		return false, err
	}
	observed, _, _, err := c.observe(ctx, w, rel)
	if err != nil {
		return false, err
	}
	parameters, err := c.policy.WindowParameters(ctx, w.ID)
	if err != nil {
		return false, err
	}
	reading, err := boundary.Boundary{
		Size: parameters.Size.Number, Confidence: parameters.Confidence.Number,
	}.Evaluate(observed)
	if err != nil {
		return false, err
	}
	return !reading.Harm, nil
}

// raisedHasShipped is whether what the incident raised has finished. An incident
// that raised no intent has nothing to wait for.
func (c *Comparison) raisedHasShipped(ctx context.Context, w Watching, i incident.Incident) (bool, error) {
	if i.IntentID == "" {
		return true, nil
	}
	return Shipped(ctx, c.pool, w.EnvironmentID, i.IntentID)
}

// Shipped is whether every item cut from one intent has shipped: a release minted
// for it, and a deploy of that release into the environment that completed. An
// intent nothing has been cut from has not shipped — the factory has not worked it
// yet.
//
// It is exported and takes the pool because two mechanisms ask it of the same
// intent and neither of them is the other. This package asks it of the intent an
// incident raised, to decide whether the incident is settled; whatever computes the
// hold a rollback leaves asks it of the rollback's revert intent, to decide whether
// the hold still stands. One function, because the two would otherwise be the same
// non-obvious predicate written twice and able to disagree about whether a revert
// has shipped.
func Shipped(ctx context.Context, pool *pgxpool.Pool, environmentID, intentID string) (bool, error) {
	if intentID == "" {
		return false, nil
	}
	items, err := item.ForIntent(ctx, pool, intentID)
	if err != nil {
		return false, err
	}
	if len(items) == 0 {
		return false, nil
	}
	for _, it := range items {
		rel, minted, err := release.ForItem(ctx, pool, it.ID)
		if err != nil {
			return false, err
		}
		if !minted {
			return false, nil
		}
		deploys, err := deploy.ByRelease(ctx, pool, environmentID, rel.ID)
		if err != nil {
			return false, err
		}
		completed := false
		for _, d := range deploys {
			if d.Status == deploy.StatusComplete {
				completed = true
			}
		}
		if !completed {
			return false, nil
		}
	}
	return true, nil
}
