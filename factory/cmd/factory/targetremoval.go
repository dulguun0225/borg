package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/deploy"
)

type targetRemovalReader struct{ pool *pgxpool.Pool }

func (r targetRemovalReader) RemovalComplete(ctx context.Context, serviceID, address string) (bool, error) {
	return deploy.TargetRemovalComplete(ctx, r.pool, serviceID, address)
}
