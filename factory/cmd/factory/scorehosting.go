package main

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/score"
)

type scoreHosting struct {
	pool *pgxpool.Pool
}

func (r scoreHosting) EnvironmentHours(ctx context.Context, itemID string) (float64, error) {
	env, found, err := environment.ForItem(ctx, r.pool, itemID)
	if err != nil || !found {
		return 0, err
	}
	cycles, err := environment.Cycles(ctx, r.pool, env.ID)
	if err != nil {
		return 0, err
	}
	return environment.EnvironmentHours(cycles, time.Now())
}

func (r scoreHosting) InstanceHours(ctx context.Context, releaseID string) (float64, error) {
	return deploy.InstanceHoursForRelease(ctx, r.pool, releaseID)
}

var _ score.LearningReaders = scoreHosting{}
