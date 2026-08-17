package postgres

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
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
// order this function names them. A package is added by writing another loop
// here and nothing else — nothing is discovered and nothing registers itself.
//
// Each statement is written so that applying it to a database that already has
// it changes nothing, so Apply may run at the start of every process, one
// process at a time. doc.go says why two at once is not the same thing.
func Apply(ctx context.Context, pool *pgxpool.Pool) error {
	for n, statement := range decisionlog.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			return fmt.Errorf("postgres: applying decisionlog statement %d: %w", n+1, err)
		}
	}
	return nil
}
