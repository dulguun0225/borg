package driftdetector

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

// MismatchTable and LastCheckTable are two of the tables this package owns,
// in a store of its own. HeadTable is the recorded chain head and
// DeliveryTable is the detector's own delivery, both added for the second
// comparison and for the detector's own page respectively.
const (
	MismatchTable  = "drift_mismatch"
	LastCheckTable = "drift_last_check"
	HeadTable      = "drift_chain_head"
	DeliveryTable  = "drift_own_delivery"
	AddressTable   = "drift_own_address"
)

// The prefixes [record.NewID] is called with for each.
const (
	MismatchIDPrefix  = "mis"
	LastCheckIDPrefix = "chk"
	HeadIDPrefix      = "hd"
	DeliveryIDPrefix  = "dly"
	AddressIDPrefix   = "adr"
)

// FormatVersionMismatch, FormatVersionLastCheck, FormatVersionHead and
// FormatVersionDelivery are written into format_version on every insert into
// the table each names.
const (
	FormatVersionMismatch  = "drift_mismatch/1"
	FormatVersionLastCheck = "drift_last_check/1"
	FormatVersionHead      = "drift_chain_head/1"
	FormatVersionDelivery  = "drift_own_delivery/1"
	FormatVersionAddress   = "drift_own_address/1"
)

// MismatchKindTarget and MismatchKindChain are the two shapes a mismatch
// takes. A target mismatch names the service and the target that
// disagreed; a chain mismatch names neither — service_id and target are
// both empty — because the log reaches every decision and so holds every
// service's production deploys at once.
const (
	MismatchKindTarget = "target"
	MismatchKindChain  = "chain"
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
// The last check is one row per production target, which the unique
// constraint enforces and [Writer.Record]'s upsert conflicts on: it is
// overwritten each pass, because what it answers is whether the drift
// detector is still running and not what it has ever said. service_id stays
// on the row for reference; it is no longer part of the key, because the
// design's record is one per target and not one per service and target.
// interval_seconds and further_pass_owed are what
// 08-drift-detection.md's "that shape is every last check record's" gives
// this one: the interval this pass runs on, and whether the writer still
// owes a further pass over this target.
//
// mismatch_kind_known and the two shapes below it are
// [MismatchKindTarget] and [MismatchKindChain]: an ordinary mismatch names
// the service and the target that disagreed, and the chain mismatch —
// raised when the second comparison finds the log's chain no longer holds
// the head this store recorded — names neither, because it holds every
// service's production deploys at once. service_id_matches_kind and
// target_matches_kind are the CHECK that enforces the split. detail is the
// chain mismatch's own words — an ordinary mismatch's [Mismatch.Why]
// composes its sentence from the other fields, and a chain mismatch has
// none of those to compose from.
var DDL = []string{
	`create table if not exists ` + MismatchTable + ` (
	` + record.Columns + `,
	kind text not null,
	service_id text not null,
	target text not null,
	running_build text not null,
	recorded_release_id text not null,
	recorded_build_id text not null,
	detail text not null,
	later_agreements int not null,
	cleared_at text not null,
	cleared_by text not null,
	` + record.Constraints + `,
	constraint mismatch_kind_known check (kind in ('` + MismatchKindTarget + `', '` + MismatchKindChain + `')),
	constraint service_id_matches_kind check ((kind = '` + MismatchKindTarget + `') = (service_id <> '')),
	constraint target_matches_kind check ((kind = '` + MismatchKindTarget + `') = (target <> '')),
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
	digest_reported boolean not null,
	interval_seconds bigint not null,
	further_pass_owed boolean not null,
	` + record.Constraints + `,
	constraint service_id_present check (service_id <> ''),
	constraint target_present check (target <> ''),
	constraint why_matches_reached check (reached or why <> ''),
	constraint interval_positive check (interval_seconds > 0),
	constraint one_row_per_target unique (target)
)`,

	// The recorded chain head is one row: singleton enforces that a second
	// pass overwrites it rather than adding a second head to keep straight
	// against. hash and seq name the newest row's own fields the pass last
	// verified; both empty and zero is the state before the detector's first
	// pass, which the second comparison reads as nothing yet to verify.
	`create table if not exists ` + HeadTable + ` (
	` + record.Columns + `,
	singleton boolean not null,
	hash text not null,
	seq bigint not null,
	` + record.Constraints + `,
	constraint singleton_is_true check (singleton),
	constraint one_head unique (singleton)
)`,

	// The detector's own delivery: a record of each delivery made to the
	// address [AddressTable] holds — 08-drift-detection.md's "installing it
	// includes writing one address into its store ... and records the
	// delivery in its own store." The address is a separate singleton row,
	// kept apart from the deliveries so setting it once at install does not
	// read as a delivery.
	`create table if not exists ` + DeliveryTable + ` (
	` + record.Columns + `,
	why text not null,
	` + record.Constraints + `,
	constraint why_present check (why <> '')
)`,

	`create table if not exists ` + AddressTable + ` (
	` + record.Columns + `,
	singleton boolean not null,
	address text not null,
	` + record.Constraints + `,
	constraint singleton_is_true check (singleton),
	constraint address_present check (address <> ''),
	constraint one_address unique (singleton)
)`,
}
