package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/service"
)

// adoptionIntent is the first item on a service whose deployer has already
// recorded reachability and whose repository has no factory release yet.
func adoptionIntent(ctx context.Context, pool *pgxpool.Pool, svc service.Service) (bool, error) {
	if !svc.Reachability.Written() {
		return false, nil
	}
	_, found, err := release.Highest(ctx, pool, svc.ID)
	return !found, err
}
