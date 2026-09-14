package deploy

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// EnvironmentTargetRemovalComplete reports whether a completed release deploy
// still marks an environment target complete. It is the reader an environment
// target-removal composition supplies.
func EnvironmentTargetRemovalComplete(ctx context.Context, pool *pgxpool.Pool, environmentID, address string) (bool, error) {
	var complete bool
	err := pool.QueryRow(ctx, `select exists (
		select 1 from `+Table+` d join `+TargetTable+` t on t.deploy_id = d.id
		where d.environment_id = $1 and d.release_id <> ''
		and t.address = $2 and t.completion = $3
	)`, environmentID, address, string(CompletionComplete)).Scan(&complete)
	if err != nil {
		return false, fmt.Errorf("deploy: reading completed target %s on environment %s: %w", address, environmentID, err)
	}
	return complete, nil
}
