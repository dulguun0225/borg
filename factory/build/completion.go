package build

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// Completion is the part of a build record known after its process has
// finished. It changes only a record already written as [RunStarted].
type Completion struct {
	Actor          record.Actor
	BuildID        string
	ArtifactDigest string
	RunState       RunState
	RunReason      string
	Results        map[string]criterion.Outcome
}

// Complete closes one started build record and records its build-process
// results in the same transaction.
func (w *Writer) Complete(ctx context.Context, completion Completion) (Build, error) {
	if err := completion.Actor.Validate(); err != nil {
		return Build{}, err
	}
	if completion.BuildID == "" {
		return Build{}, ErrNotFound
	}
	if completion.RunState != RunCompleted && completion.RunState != RunDidNotRun {
		return Build{}, fmt.Errorf("build: unknown completion state %q", completion.RunState)
	}
	if completion.RunState == RunCompleted && completion.ArtifactDigest == "" {
		return Build{}, ErrArtifactDigestEmpty
	}
	if completion.RunState == RunDidNotRun && completion.RunReason == "" {
		return Build{}, ErrRunReasonEmpty
	}
	if completion.RunState == RunDidNotRun && completion.ArtifactDigest != "" {
		return Build{}, fmt.Errorf("build: a did-not-run completion has an artifact")
	}

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return Build{}, fmt.Errorf("build: beginning completion of %s: %w", completion.BuildID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, w.token); err != nil {
		return Build{}, err
	}
	result, err := tx.Exec(ctx, `update `+Table+` set run_state = $1, run_reason = $2, artifact_digest = $3 where id = $4 and run_state = $5`,
		completion.RunState, completion.RunReason, completion.ArtifactDigest, completion.BuildID, RunStarted)
	if err != nil {
		return Build{}, fmt.Errorf("build: completing %s: %w", completion.BuildID, err)
	}
	if result.RowsAffected() != 1 {
		return Build{}, fmt.Errorf("build: %s is not a started record", completion.BuildID)
	}
	if len(completion.Results) > 0 {
		run := criterion.Run{BuildID: completion.BuildID, Number: 0, Place: criterion.PlaceBuild}
		if err := criterion.InsertResults(ctx, tx, completion.Actor, run, completion.Results); err != nil {
			return Build{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Build{}, fmt.Errorf("build: committing completion of %s: %w", completion.BuildID, err)
	}
	return Get(ctx, w.pool, completion.BuildID)
}
