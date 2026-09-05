package deploy

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Resume is the deployer's restart: every component's restart is a read of its
// own records, and the deployer's is the deploy records no target has finished.
//
// A record every target of which is complete is completed — the deployer stopped
// between the last target and the record's own advance. A record no target
// reached is returned: it is marked failed at [StepStopped], which is where a
// failure stands for Ops, what a second restart leaves alone, and what both
// queries over overlapping windows descend past. A record with some targets
// complete and some not is neither: the design admits no failed record with a
// target complete, so it stays started as the recorded partial deploy it is, and
// Resume returns those so the caller can carry on reaching the rest.
//
// A failed record is not read at all. What Resume reads is the started ones.
func Resume(ctx context.Context, w *Writer) ([]Deploy, error) {
	unfinished, err := Unfinished(ctx, w.Pool())
	if err != nil {
		return nil, err
	}

	var partial []Deploy
	for _, d := range unfinished {
		targets, err := Targets(ctx, w.Pool(), d.ID)
		if err != nil {
			return nil, err
		}
		complete := 0
		for _, target := range targets {
			if target.Completion == CompletionComplete {
				complete++
			}
		}
		switch {
		case len(targets) > 0 && complete == len(targets):
			if err := w.Complete(ctx, d.ID); err != nil {
				return nil, err
			}
		case complete == 0:
			if err := w.MarkFailed(ctx, d.ID, StepStopped); err != nil {
				return nil, err
			}
		default:
			partial = append(partial, d)
		}
	}
	return partial, nil
}

// Partial is the targets of a deploy that are not complete, in the
// environment's order, which is what a caller resuming a recorded partial deploy
// reaches next.
func Partial(ctx context.Context, pool *pgxpool.Pool, deployID string) ([]Target, error) {
	targets, err := Targets(ctx, pool, deployID)
	if err != nil {
		return nil, err
	}
	var owed []Target
	for _, target := range targets {
		if target.Completion != CompletionComplete {
			owed = append(owed, target)
		}
	}
	return owed, nil
}
