package deploy

import (
	"context"
	"fmt"
	"time"
)

// The hold between one target and the next. A target is not reached until the
// target before it is marked complete and the targets already reached have
// served the bake volume, which is a volume and not a period because the
// analysis window is a volume condition and a period on a quiet service
// measures nothing.

// Bake is the hold between one target and the next: the traffic the targets
// already reached have served, and whether the window's cap has run. The
// deployer holds until the first reaches the volume or the second is true —
// once the cap has run the window closes timed out and the remaining targets are
// reached with no hold between them, since a quiet service that never serves the
// bake volume would otherwise never complete a deploy.
//
// The health monitor is what can answer both, reading the emission and the
// window it opened at this deploy. Nothing implements this yet; a rollout given
// none holds nowhere, and doc.go says so.
type Bake interface {
	// Served is how much the targets reached so far have served since this
	// deploy began, and whether the window's cap has run.
	Served(ctx context.Context, deployID string) (volume int64, capRun bool, err error)
}

// DefaultBakePoll is how often a rollout asks whether the targets already
// reached have served the bake volume. A volume is not a period, so the hold
// cannot be a sleep of a known length: it is a read repeated until the answer
// changes.
const DefaultBakePoll = time.Second

// hold is the bake volume between one target and the next. It returns as soon as
// the targets already reached have served the volume, or as soon as the window's
// cap has run, whichever comes first — and at once where the caller supplied no
// way to ask.
func hold(ctx context.Context, p Performance, deployID string) error {
	if p.Bake == nil || p.BakeVolume <= 0 {
		return nil
	}
	poll := p.BakePoll
	if poll <= 0 {
		poll = DefaultBakePoll
	}
	for {
		served, capRun, err := p.Bake.Served(ctx, deployID)
		if err != nil {
			return fmt.Errorf("deploy: reading what %s has served: %w", deployID, err)
		}
		if capRun || served >= p.BakeVolume {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}
	}
}
