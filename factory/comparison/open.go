package comparison

import (
	"context"
	"errors"
	"fmt"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/window"
)

// ErrScoreVersionMissing is returned by [Comparison.Open] for an opening naming no
// score version. The window stores the two versions in force at the open the way a
// gate's opening row does, and the score's is the caller's to hand over — the
// comparison does not append one.
var ErrScoreVersionMissing = errors.New("comparison: a window names the score version in force at the open")

// Open opens the watch window over one production deploy, and returns false where
// no window opens. A window is one per production deploy of a release the service
// has not watched before, whichever attempt that is — so a rollback opens none, the
// release it returns to having been watched already, and neither does a redeploy of
// one already watched.
//
// The size, the confidence, and the cap in force are read here and copied onto the
// record, which is what makes a reading at an exit interpretable: an owner
// re-authoring a size while the window is open would otherwise change what a window
// already closed is read to have meant. So is whether clean is available, which is
// a fact of the open and never changes — a release with nothing below it has no
// baseline, so nothing about it is ruled out by watching and its window ends at the
// cap.
//
// heldOut is the other way clean becomes unavailable, and it is the caller's to
// hand over for the reason the score version is: what the score selected is read
// off the decisions on the item, which this package does not read. A held-out
// release runs to the cap rather than stopping where the boundary would allow —
// auto-passing a change the score wanted gated is where the factory is most openly
// guessing, so it takes the longest watch available, which on a quiet service is
// not much. Both facts go on the record, because a window that ran to the cap for
// want of a baseline and one that ran to the cap because it was measuring what the
// factory guessed at are not the same window.
func (c *Comparison) Open(ctx context.Context, w Watching, deployID, releaseID, scoreVersion string, heldOut bool) (window.Window, bool, error) {
	if err := w.validate(); err != nil {
		return window.Window{}, false, err
	}
	if scoreVersion == "" {
		return window.Window{}, false, ErrScoreVersionMissing
	}

	watched, found, err := window.ForRelease(ctx, c.pool, releaseID)
	if err != nil {
		return window.Window{}, false, err
	}
	if found {
		return watched, false, nil
	}

	rel, err := release.Get(ctx, c.pool, releaseID)
	if err != nil {
		return window.Window{}, false, err
	}
	below, err := release.Below(ctx, c.pool, w.ID, rel.Number)
	if err != nil {
		return window.Window{}, false, err
	}
	parameters, err := c.policy.WindowParameters(ctx, w.ID)
	if err != nil {
		return window.Window{}, false, err
	}
	version, err := policy.InForce(ctx, c.pool)
	if err != nil {
		return window.Window{}, false, err
	}

	opened, err := c.windows.Open(ctx, Actor, window.Opening{
		DeployID:       deployID,
		ReleaseID:      releaseID,
		ServiceID:      w.ID,
		CleanAvailable: below && !heldOut,
		HeldOut:        heldOut,
		Size:           parameters.Size.Number,
		Confidence:     parameters.Confidence.Number,
		CapSeconds:     parameters.CapSeconds.Number,
		Formula:        boundary.Formula,
		PolicyVersion:  version.ID,
		ScoreVersion:   scoreVersion,
	})
	if err != nil {
		return window.Window{}, false, err
	}
	return opened, true, nil
}

// Room is whether the service may open another window, how many it holds open, and
// what K is in force. An open window blocks nothing until K of them are open, and
// then the next production deploy holds — a wait on the factory, which does not
// page, so it shows only to a reader who asks.
//
// It is here rather than in whatever computes the hold, because the K in force and
// the count of open windows are the two halves of one reading and a caller holding
// only one of them would report a hold it cannot explain.
func (c *Comparison) Room(ctx context.Context, serviceID string) (bool, int, int, error) {
	if serviceID == "" {
		return false, 0, 0, fmt.Errorf("%w: K is per service, and none is named", ErrWatchingIncomplete)
	}
	open, err := window.CountOpen(ctx, c.pool, serviceID)
	if err != nil {
		return false, 0, 0, err
	}
	parameters, err := c.policy.WindowParameters(ctx, serviceID)
	if err != nil {
		return false, 0, 0, err
	}
	k := int(parameters.K.Number)
	return open < k, open, k, nil
}
