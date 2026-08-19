package postgres

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/factorypolicy"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/pin"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/service"
)

// DefaultURL is the development database factory/docker-compose.yml runs. The
// port is 5433 so that a PostgreSQL installed on the machine keeps 5432.
const DefaultURL = "postgres://factory:factory@localhost:5433/factory"

// URLEnv is the environment variable that names the database.
const URLEnv = "DATABASE_URL"

// URL is the database to open: [URLEnv] where it is set, [DefaultURL]
// otherwise.
func URL() string {
	if url := os.Getenv(URLEnv); url != "" {
		return url
	}
	return DefaultURL
}

// Open returns a pool over url and reaches the database once before it
// returns, so an unreachable database is an error here and not at the first
// query.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("postgres: opening the pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: reaching the database: %w", err)
	}
	return pool, nil
}

// Apply creates the factory's schema: every package that owns a table, in the
// order this function names them. A package is added by writing another line
// into the list here and nothing else — nothing is discovered and nothing
// registers itself.
//
// Each statement is written so that applying it to a database that already has
// it changes nothing, so Apply may run at the start of every process, one
// process at a time. doc.go says why two at once is not the same thing.
func Apply(ctx context.Context, pool *pgxpool.Pool) error {
	for _, owner := range []struct {
		name string
		ddl  []string
	}{
		{"decisionlog", decisionlog.DDL},
		{"intent", intent.DDL},
		{"service", service.DDL},
		{"item", item.DDL},
		{"criterion", criterion.DDL},
		{"artifact", artifact.DDL},
		{"build", build.DDL},
		{"release", release.DDL},
		{"deploy", deploy.DDL},
		{"area", area.DDL},
		{"environment", environment.DDL},
		{"factorypolicy", factorypolicy.DDL},
		{"pin", pin.DDL},
		{"score", score.DDL},
		{"policy", policy.DDL},
	} {
		for n, statement := range owner.ddl {
			if _, err := pool.Exec(ctx, statement); err != nil {
				return fmt.Errorf("postgres: applying %s statement %d: %w", owner.name, n+1, err)
			}
		}
	}
	return nil
}
