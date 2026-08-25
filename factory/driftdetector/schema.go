package driftdetector

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

// MismatchTable and LastCheckTable are the two tables this package owns, in a
// store of its own.
const (
	MismatchTable  = "drift_mismatch"
	LastCheckTable = "drift_last_check"
)

// The prefixes [record.NewID] is called with for each.
const (
	MismatchIDPrefix  = "mis"
	LastCheckIDPrefix = "chk"
)

// DefaultURL is the drift detector's own store as the demonstration runs it: the
// development database with a schema of its own. A second schema on one PostgreSQL
// is where this store's independence is weakest and it is stated rather than hidden —
// a store the owner installs elsewhere is this code with a different URL, and nothing
// in the factory writes either way.
const DefaultURL = "postgres://factory:factory@localhost:5433/factory?search_path=driftdetector"

// URLEnv is the environment variable that names the drift detector's
// store. It is a variable of its own and not the factory's, so pointing the
// drift detector somewhere else is one setting and not a change to the
// factory's configuration.
const URLEnv = "DRIFTDETECTOR_DATABASE_URL"

// URL is the store to open: [URLEnv] where it is set, [DefaultURL] otherwise.
func URL() string {
	if url := os.Getenv(URLEnv); url != "" {
		return url
	}
	return DefaultURL
}

// Open returns a pool over url and reaches the store once before it returns, so an
// unreachable store is an error here and not at the first query.
//
// It is this package's own rather than the factory's opener, and doc.go says why:
// the factory's applies every record package's schema, and a store the factory
// applies is a store the factory owns.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("driftdetector: opening the pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("driftdetector: reaching its store: %w", err)
	}
	return pool, nil
}

// Apply creates the drift detector's schema. It is called by the
// drift detector's own process and by nothing in the factory.
func Apply(ctx context.Context, pool *pgxpool.Pool) error {
	for n, statement := range DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			return fmt.Errorf("driftdetector: applying statement %d: %w", n+1, err)
		}
	}
	return nil
}

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than restated, so
// these two rows carry the actor and the timestamp every record in the factory does —
// which is seam 1 and is about who wrote a row, not about which store it is in.
//
// The mismatch has no chain behind it, which the design states as a cost: the trail
// of what stopped a deploy is complete only when this store is read beside the log.
//
// cleared_together is what keeps a cleared mismatch from being half cleared: the
// time and the human arrive together, and a mismatch with one of them is refused.
// later_agreements counts the passes that agreed after the mismatch was written, so a
// human clearing one can see whether the disagreement persisted.
//
// The last check is one row per service and target, which the unique constraint
// enforces and [Writer.Record]'s upsert conflicts on: it is overwritten each
// pass, because what it answers is whether the drift detector is still
// running and not what it has ever said.
var DDL = []string{
	`create table if not exists ` + MismatchTable + ` (
	` + record.Columns + `,
	service_id text not null,
	target text not null,
	running_build text not null,
	recorded_release_id text not null,
	recorded_build_id text not null,
	later_agreements int not null,
	cleared_at text not null,
	cleared_by text not null,
	` + record.Constraints + `,
	constraint service_id_present check (service_id <> ''),
	constraint target_present check (target <> ''),
	constraint later_agreements_not_negative check (later_agreements >= 0),
	constraint cleared_together check ((cleared_at <> '') = (cleared_by <> '')),
	constraint cleared_at_is_time_layout check (cleared_at = '' or cleared_at ~ '` + record.TimePattern + `')
)`,

	`create table if not exists ` + LastCheckTable + ` (
	` + record.Columns + `,
	service_id text not null,
	target text not null,
	reached boolean not null,
	why text not null,
	running_build text not null,
	recorded_release_id text not null,
	recorded_build_id text not null,
	agreed boolean not null,
	` + record.Constraints + `,
	constraint service_id_present check (service_id <> ''),
	constraint target_present check (target <> ''),
	constraint why_matches_reached check (reached or why <> ''),
	constraint one_row_per_service_and_target unique (service_id, target)
)`,
}
